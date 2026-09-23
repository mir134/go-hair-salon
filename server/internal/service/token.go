package service

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// DefaultTokenTTL 是登录 JWT 的默认有效期（todo 10：24h）。
const DefaultTokenTTL = 24 * time.Hour

// TokenClaims 是 JWT 负载：只携带用户标识与角色声明。
//
// role 仅用于展示/调试参考；每一次请求的权限判定都必须以数据库实时值为准
// （04-API.md:60-68），禁止信任 token 中的角色声明。
// 禁止在 claims 中写入密码哈希等任何敏感信息。
type TokenClaims struct {
	UserID int64  `json:"uid"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// TokenService 负责签发与解析 HS256 JWT。
type TokenService struct {
	secret []byte
	ttl    time.Duration
}

// NewTokenService 构造 token 服务；ttl 为 0 时使用 DefaultTokenTTL。
func NewTokenService(secret string, ttl time.Duration) *TokenService {
	if ttl == 0 {
		ttl = DefaultTokenTTL
	}
	return &TokenService{secret: []byte(secret), ttl: ttl}
}

// Issue 为用户签发 JWT，返回 token 与过期时间。
func (s *TokenService) Issue(user *model.User) (string, time.Time, error) {
	if len(s.secret) == 0 {
		return "", time.Time{}, errors.New("JWT 密钥未配置")
	}
	now := time.Now().UTC()
	expiresAt := now.Add(s.ttl)
	claims := &TokenClaims{
		UserID: user.ID,
		Role:   user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(user.ID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("签发 JWT 失败: %w", err)
	}
	return signed, expiresAt, nil
}

// Parse 校验签名与有效期并解析 claims；任何失败都表示凭证不可用（调用方映射 401）。
func (s *TokenService) Parse(tokenString string) (*TokenClaims, error) {
	claims := &TokenClaims{}
	_, err := jwt.ParseWithClaims(tokenString, claims, func(*jwt.Token) (any, error) {
		return s.secret, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, fmt.Errorf("解析 JWT 失败: %w", err)
	}
	return claims, nil
}
