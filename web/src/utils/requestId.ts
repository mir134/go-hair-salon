/**
 * 客户端幂等键生成（04-API.md:283-286）：创建订单/充值必须携带 request_id（UUID）。
 *
 * 优先 `crypto.randomUUID`（仅安全上下文可用）。门店局域网内店员用手机以
 * `http://<内网IP>:8080` 访问时不是安全上下文，`randomUUID` 不可用；此时回退
 * `crypto.getRandomValues`（非安全上下文同样可用）手工拼装 RFC 4122 v4。
 *
 * 生成时机由调用方控制：同一次「提交尝试」只生成一次，失败重试复用同一值
 * （后端据此判定重复提交，避免重复入账）。
 */
export function generateRequestId(): string {
  const webCrypto = globalThis.crypto
  if (typeof webCrypto.randomUUID === 'function') {
    return webCrypto.randomUUID()
  }
  const values = Array.from(webCrypto.getRandomValues(new Uint8Array(16)))
  // 版本位（第 7 字节高 4 位 = 0100）与变体位（第 9 字节高 2 位 = 10）
  values[6] = ((values[6] ?? 0) & 0x0f) | 0x40
  values[8] = ((values[8] ?? 0) & 0x3f) | 0x80
  const groups = [
    values.slice(0, 4),
    values.slice(4, 6),
    values.slice(6, 8),
    values.slice(8, 10),
    values.slice(10, 16),
  ]
  return groups.map((group) => group.map(toHex).join('')).join('-')
}

function toHex(value: number): string {
  return value.toString(16).padStart(2, '0')
}
