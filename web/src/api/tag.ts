import { del, get, post } from './http'

/**
 * 标签 DTO，对齐 server/internal/controller/tag.go 的 TagView。
 *
 * 客户详情返回的标签可能包含 `deleted=true` 的历史标签（删标签不级联删历史关系，
 * 03-DATABASE.md:77）；GET /tags 只返回未删除标签。
 */
export interface Tag {
  id: number
  name: string
  color: string
  deleted: boolean
  created_at: string
  updated_at: string
}

/** GET /tags（both）：全部未删除标签，按 id 升序 */
export function listTags(): Promise<Tag[]> {
  return get<Tag[]>('/tags')
}

/** POST /customers/:id/tags（编辑客户 both）：挂标签，返回客户最新标签列表 */
export function attachCustomerTag(customerId: number, tagId: number): Promise<Tag[]> {
  return post<Tag[]>(`/customers/${customerId}/tags`, { tag_id: tagId })
}

/** DELETE /customers/:id/tags/:tag_id（编辑客户 both）：摘标签，返回客户最新标签列表 */
export function detachCustomerTag(customerId: number, tagId: number): Promise<Tag[]> {
  return del<Tag[]>(`/customers/${customerId}/tags/${tagId}`)
}
