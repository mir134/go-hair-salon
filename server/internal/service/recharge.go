package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// RechargeService 负责充值事务（03-DATABASE.md:164-186,294-307、06 §4:46-54）。
//
// 单事务内完成：recharge_record + 本金/赠送两条 balance_transactions
// （balance_before/after 连续）+ customers.balance_cents 原子累加；
// 任一步失败整体回滚（零部分写入）；事务提交后写 operation_logs(action=recharge)。
//
// 余额增加 = 本金 + 赠送（03-DATABASE.md:172）；actual_amount_cents 是资金流入口径，
// 不参与余额计算（03-DATABASE.md:170）。
type RechargeService struct {
	tx        *repository.Transactor
	recharges *repository.RechargeRepository
	customers *repository.CustomerRepository
	ledger    *repository.LedgerRepository
	logs      *OperationLogService
}

// RechargeServiceDeps 是充值服务的依赖集合。
type RechargeServiceDeps struct {
	Tx        *repository.Transactor
	Recharges *repository.RechargeRepository
	Customers *repository.CustomerRepository
	Ledger    *repository.LedgerRepository
	Logs      *OperationLogService
}

// NewRechargeService 构造充值服务。
func NewRechargeService(deps RechargeServiceDeps) *RechargeService {
	return &RechargeService{
		tx:        deps.Tx,
		recharges: deps.Recharges,
		customers: deps.Customers,
		ledger:    deps.Ledger,
		logs:      deps.Logs,
	}
}

// RechargeInput 是充值输入（字段与 04-API.md:157-175 语义对齐）。
//
// ActualAmountCents 为 nil 表示实付 = 本金；存在充值优惠时可显式传入更小值。
type RechargeInput struct {
	RequestID           string
	CustomerID          int64
	RechargeAmountCents int64
	GiftAmountCents     int64
	ActualAmountCents   *int64
	PaymentMethod       string
	Remark              string
}

// RechargeResult 是充值结果。
//
// Created=false 表示命中 request_id 幂等键：返回原记录，本次未写入任何数据。
type RechargeResult struct {
	Record       *model.RechargeRecord
	CustomerName string
	Created      bool
}

// RechargeListQuery 是 GET /recharges 查询输入（04-API.md:157-165）。
//
// 日期为 YYYY-MM-DD（UTC 日期边界，含当日），非法格式宽松回退为不过滤。
type RechargeListQuery struct {
	CustomerID int64
	StartDate  string
	EndDate    string
	PageQuery
}

// RechargeListRow 是充值列表读取行（仓储联表结果的类型别名，避免重复建模）。
type RechargeListRow = repository.RechargeRow

// ListRecharges 按条件分页查询充值记录（时间倒序，最新在前）。
func (s *RechargeService) ListRecharges(ctx context.Context, q RechargeListQuery) (*PageResult[RechargeListRow], error) {
	startAt, endAt := parseDateRange(q.StartDate, q.EndDate)
	offset, limit, page, pageSize := q.Normalize()
	rows, total, err := s.recharges.List(ctx, repository.RechargeListFilter{
		CustomerID: q.CustomerID,
		StartAt:    startAt,
		EndAt:      endAt,
		Offset:     offset,
		Limit:      limit,
	})
	if err != nil {
		return nil, fmt.Errorf("查询充值列表失败: %w", err)
	}
	return &PageResult[RechargeListRow]{Items: rows, Total: total, Page: page, PageSize: pageSize}, nil
}

// CreateRecharge 充值（04-API.md:167-174）：
//
//  1. 校验 request_id / 客户存在 / 本金 > 0 / 赠送 ≥ 0 / 实付 ≥ 0 / 支付方式合法；
//  2. request_id 幂等：已存在则返回原记录（调用方渲染 200），不重复入账；
//  3. 单事务：写充值记录 → 本金流水（type=recharge）→ 赠送 > 0 时第二条
//     流水（type=gift，before/after 连续）→ 余额原子累加（本金 + 赠送）；
//  4. 事务提交后写 operation_logs(action=recharge)。
func (s *RechargeService) CreateRecharge(ctx context.Context, in RechargeInput) (*RechargeResult, error) {
	requestID, err := validateRequestID(in.RequestID)
	if err != nil {
		return nil, err
	}
	if in.CustomerID <= 0 {
		return nil, BadRequest("客户不能为空")
	}
	if in.RechargeAmountCents <= 0 {
		return nil, BadRequest("充值本金必须大于 0（整数分）")
	}
	if in.GiftAmountCents < 0 {
		return nil, BadRequest("赠送金额不能为负")
	}
	actual := in.RechargeAmountCents
	if in.ActualAmountCents != nil {
		actual = *in.ActualAmountCents
		if actual < 0 {
			return nil, BadRequest("实付金额不能为负")
		}
	}
	if !isValidPaymentMethod(in.PaymentMethod) {
		return nil, BadRequest("支付方式不合法")
	}

	customer, err := s.customers.FindByID(ctx, in.CustomerID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrCustomerNotFound
		}
		return nil, fmt.Errorf("查询客户失败: %w", err)
	}

	// 幂等快路径：同一 request_id 直接返回原记录（唯一索引是并发兜底）。
	existing, err := s.findByRequestID(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	record := &model.RechargeRecord{
		CustomerID:          customer.ID,
		RequestID:           requestID,
		RechargeAmountCents: in.RechargeAmountCents,
		GiftAmountCents:     in.GiftAmountCents,
		ActualAmountCents:   actual,
		PaymentMethod:       in.PaymentMethod,
		Status:              model.RechargeStatusActive,
		OperatorID:          OperatorIDPtr(ctx),
		Remark:              in.Remark,
	}
	if err := s.tx.WithinTx(ctx, func(tx repository.Tx) error {
		return s.createRechargeTx(ctx, tx, record)
	}); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			// 并发兜底：唯一索引已阻止重复入账 → 返回原记录。
			existing, findErr := s.findByRequestID(ctx, requestID)
			if findErr != nil {
				return nil, findErr
			}
			if existing != nil {
				return existing, nil
			}
			return nil, Conflict("充值记录已存在，请勿重复提交")
		}
		return nil, err
	}

	// 事务提交后写审计日志（禁止在事务内写，见 OperationLogService 注释）。
	s.writeRechargeLog(ctx, record)
	return &RechargeResult{Record: record, CustomerName: customer.Name, Created: true}, nil
}

// createRechargeTx 单事务写入充值记录 + 本金/赠送流水 + 余额缓存（03-DATABASE.md:294-307）。
func (s *RechargeService) createRechargeTx(ctx context.Context, tx repository.Tx, record *model.RechargeRecord) error {
	if err := s.recharges.CreateTx(ctx, tx, record); err != nil {
		return err
	}
	// 余额必须在事务内、原子累加之前读取：仅用于流水 before/after，
	// 入账结果由 ApplyBalanceDeltaTx 的原子 UPDATE 决定。
	balanceBefore, err := s.customers.BalanceTx(ctx, tx, record.CustomerID)
	if err != nil {
		return fmt.Errorf("读取充值前余额失败: %w", err)
	}

	rows := []model.BalanceTransaction{{
		CustomerID:         record.CustomerID,
		Type:               model.BalanceTxRecharge,
		AmountCents:        record.RechargeAmountCents,
		BalanceBeforeCents: balanceBefore,
		BalanceAfterCents:  balanceBefore + record.RechargeAmountCents,
		ReferenceType:      model.ReferenceTypeRecharge,
		ReferenceID:        &record.ID,
		OperatorID:         record.OperatorID,
		Remark:             record.Remark,
	}}
	if record.GiftAmountCents > 0 {
		// 赠送流水与本金流水连续：before = 本金流水的 after（06 §4:51）。
		rows = append(rows, model.BalanceTransaction{
			CustomerID:         record.CustomerID,
			Type:               model.BalanceTxGift,
			AmountCents:        record.GiftAmountCents,
			BalanceBeforeCents: balanceBefore + record.RechargeAmountCents,
			BalanceAfterCents:  balanceBefore + record.RechargeAmountCents + record.GiftAmountCents,
			ReferenceType:      model.ReferenceTypeRecharge,
			ReferenceID:        &record.ID,
			OperatorID:         record.OperatorID,
			Remark:             record.Remark,
		})
	}
	for i := range rows {
		if err := s.ledger.CreateBalanceTx(ctx, tx, &rows[i]); err != nil {
			return fmt.Errorf("写入余额流水失败: %w", err)
		}
	}

	// 余额增加 = 本金 + 赠送（03-DATABASE.md:172）；原子累加，禁止先查后写。
	applied, err := s.customers.ApplyBalanceDeltaTx(ctx, tx, record.CustomerID,
		record.RechargeAmountCents+record.GiftAmountCents)
	if err != nil {
		return fmt.Errorf("更新客户余额失败: %w", err)
	}
	if !applied {
		return ErrCustomerNotFound
	}
	return nil
}

// findByRequestID 查询幂等键对应的原记录；不存在返回 (nil, nil)。
func (s *RechargeService) findByRequestID(ctx context.Context, requestID string) (*RechargeResult, error) {
	record, err := s.recharges.FindByRequestID(ctx, requestID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("查询幂等充值记录失败: %w", err)
	}
	name, err := s.customerName(ctx, record.CustomerID)
	if err != nil {
		return nil, err
	}
	return &RechargeResult{Record: record, CustomerName: name, Created: false}, nil
}

// customerName 读取客户姓名；客户已软删除时返回空名（历史记录仍可幂等返回）。
func (s *RechargeService) customerName(ctx context.Context, customerID int64) (string, error) {
	customer, err := s.customers.FindByID(ctx, customerID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("查询客户失败: %w", err)
	}
	return customer.Name, nil
}

// writeRechargeLog 在事务提交后写 recharge 审计日志。
//
// 06 §4:52：充值必须记录操作人、支付方式、流水号/幂等键；
// 日志写入失败不阻断已提交的交易（与既有记账策略一致）。
func (s *RechargeService) writeRechargeLog(ctx context.Context, record *model.RechargeRecord) {
	content := fmt.Sprintf("充值本金 %d 分，赠送 %d 分，实付 %d 分，支付方式 %s，幂等键 %s",
		record.RechargeAmountCents, record.GiftAmountCents, record.ActualAmountCents,
		record.PaymentMethod, record.RequestID)
	if err := s.logs.WriteLog(ctx, "recharge", "recharge", record.ID, content); err != nil {
		slog.Default().Error("写入充值审计日志失败", "recharge_id", record.ID, "err", err)
	}
}
