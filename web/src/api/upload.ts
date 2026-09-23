import { post } from './http'

/**
 * 上传成功返回（server/internal/controller/upload.go 的 UploadView）：
 * url 为可直接用于 <img src> 的相对路径（/uploads/<sha256[0:16]>.<ext>），
 * name 为服务端哈希存储名，size 为字节数。
 */
export interface UploadResult {
  url: string
  name: string
  size: number
}

/** 允许上传的图片类型（与 server/internal/service/upload.go 白名单一致） */
export const UPLOAD_ACCEPT = 'image/jpeg,image/png,image/webp,image/gif'

/** 单张头像大小上限（字节），与后端 MaxUploadSizeBytes 对齐（2MB） */
export const UPLOAD_MAX_SIZE_BYTES = 2 * 1024 * 1024

/**
 * POST /uploads（multipart/form-data，文件字段名 file）：
 * 后端按 magic bytes + 扩展名双重校验并做哈希重命名，返回服务端生成的相对 URL；
 * 头像表单只需把返回的 url 存入 avatar 字段。
 */
export function uploadImage(file: File): Promise<UploadResult> {
  const form = new FormData()
  form.append('file', file)
  return post<UploadResult>('/uploads', form)
}
