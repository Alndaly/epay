import { Field, FieldContent, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { SECRET_MASK } from "@/lib/channel-options"
import type { DriverField as DriverFieldSpec } from "@/lib/types"

/**
 * 根据驱动的字段描述渲染对应的输入控件。
 * 新增渠道驱动时只需在后端声明字段，这里无需修改。
 */
export function DriverField({
  field,
  value,
  onChange,
}: {
  field: DriverFieldSpec
  value: unknown
  onChange: (value: unknown) => void
}) {
  const id = `field-${field.key}`
  const help =
    field.secret && value === SECRET_MASK
      ? `已保存，保持 ${SECRET_MASK} 不变即沿用原值。${field.help ?? ""}`
      : field.help

  if (field.type === "switch") {
    return (
      <Field orientation="horizontal">
        <FieldContent>
          <FieldLabel htmlFor={id}>{field.label}</FieldLabel>
          {help && <FieldDescription>{help}</FieldDescription>}
        </FieldContent>
        <Switch id={id} checked={Boolean(value)} onCheckedChange={onChange} />
      </Field>
    )
  }

  const text = Array.isArray(value) ? value.join(", ") : String(value ?? "")
  let control: React.ReactNode
  switch (field.type) {
    case "select":
      control = (
        <Select value={text} onValueChange={onChange}>
          <SelectTrigger id={id}>
            <SelectValue placeholder="请选择" />
          </SelectTrigger>
          <SelectContent>
            {field.options?.map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )
      break
    case "textarea":
      control = (
        <Textarea
          id={id}
          value={text}
          placeholder={field.placeholder}
          onChange={(e) => onChange(e.target.value)}
          onFocus={(e) => field.secret && text === SECRET_MASK && e.currentTarget.select()}
          spellCheck={false}
        />
      )
      break
    case "number":
      control = (
        <Input
          id={id}
          type="number"
          step="any"
          value={text}
          placeholder={field.placeholder}
          onChange={(e) => onChange(e.target.value === "" ? "" : Number(e.target.value))}
        />
      )
      break
    default: // text / tags
      control = (
        <Input
          id={id}
          type={field.secret ? "password" : "text"}
          value={text}
          placeholder={field.placeholder}
          autoComplete="off"
          onChange={(e) => onChange(e.target.value)}
          onFocus={(e) => field.secret && text === SECRET_MASK && e.currentTarget.select()}
        />
      )
  }

  return (
    <Field>
      <FieldLabel htmlFor={id}>
        {field.label}
        {field.required && " *"}
      </FieldLabel>
      {control}
      {help && <FieldDescription>{help}</FieldDescription>}
    </Field>
  )
}
