import type { DriverField } from "@/lib/types"

/** 后端对已保存的敏感字段返回该占位符；原样提交表示保持不变。 */
export const SECRET_MASK = "******"

/** 字段的初始值：优先使用已保存的值，其次是驱动声明的默认值。 */
export function initialValue(field: DriverField, saved: unknown): unknown {
  if (saved !== undefined && saved !== null) return saved
  if (field.default !== undefined) return field.default
  return field.type === "switch" ? false : ""
}
