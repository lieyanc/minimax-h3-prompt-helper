import { useEffect, useState } from "react"
import { CornerDownLeftIcon, WandSparklesIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
  FieldTitle,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group"
import { Textarea } from "@/components/ui/textarea"
import type { Question } from "@/lib/api"

/**
 * Renders one batch of slot questions. Suggestions coming from the vision model
 * are pre-filled so the common case is "confirm and continue".
 */
export function QuestionForm({
  questions,
  busy,
  onSubmit,
}: {
  questions: Question[]
  busy: boolean
  onSubmit: (answers: Record<string, string>) => void
}) {
  const [values, setValues] = useState<Record<string, string>>({})

  useEffect(() => {
    const next: Record<string, string> = {}
    for (const q of questions) next[q.slot] = q.suggestion ?? ""
    setValues(next)
  }, [questions])

  const set = (slot: string, value: string) =>
    setValues((prev) => ({ ...prev, [slot]: value }))

  const missing = questions.filter(
    (q) => q.required && !(values[q.slot] ?? "").trim()
  )

  return (
    <form
      className="flex flex-col gap-6"
      onSubmit={(e) => {
        e.preventDefault()
        if (missing.length > 0 || busy) return
        const filled: Record<string, string> = {}
        for (const q of questions) {
          const v = (values[q.slot] ?? "").trim()
          if (v) filled[q.slot] = v
        }
        onSubmit(filled)
      }}
    >
      <FieldGroup>
        {questions.map((q) => (
          <QuestionField
            key={q.slot}
            question={q}
            value={values[q.slot] ?? ""}
            onChange={(v) => set(q.slot, v)}
          />
        ))}
      </FieldGroup>

      <div className="flex items-center gap-3">
        <Button type="submit" disabled={busy || missing.length > 0}>
          <CornerDownLeftIcon data-icon="inline-start" />
          提交这一轮
        </Button>
        {missing.length > 0 ? (
          <span className="text-xs text-muted-foreground">
            还有 {missing.length} 个必答项没填
          </span>
        ) : null}
      </div>
    </form>
  )
}

function QuestionField({
  question,
  value,
  onChange,
}: {
  question: Question
  value: string
  onChange: (v: string) => void
}) {
  const suggested = (question.suggestion ?? "").trim()
  const showSuggestion = suggested !== "" && suggested !== value.trim()

  if (question.kind === "choice") {
    const known = (question.options ?? []).some((o) => o.value === value)
    return (
      <FieldSet>
        <FieldLegend className="flex flex-wrap items-center gap-2">
          {question.title}
          {question.required ? null : (
            <span className="text-xs font-normal text-muted-foreground">
              可跳过
            </span>
          )}
        </FieldLegend>
        {question.help ? (
          <FieldDescription>{question.help}</FieldDescription>
        ) : null}
        <RadioGroup
          value={known ? value : ""}
          onValueChange={(v) => onChange(String(v ?? ""))}
          className="sm:grid-cols-2"
        >
          {(question.options ?? []).map((opt) => {
            const id = `${question.slot}-${opt.value || "empty"}`
            return (
              <FieldLabel key={id} htmlFor={id}>
                <Field orientation="horizontal">
                  <RadioGroupItem id={id} value={opt.value} />
                  <FieldContent>
                    <FieldTitle>{opt.label}</FieldTitle>
                    {opt.desc ? (
                      <FieldDescription>{opt.desc}</FieldDescription>
                    ) : null}
                  </FieldContent>
                </Field>
              </FieldLabel>
            )
          })}
        </RadioGroup>
        {question.allowFree ? (
          <Field>
            <FieldDescription>
              也可以直接写一个列表以外的值
            </FieldDescription>
            <Input
              value={known ? "" : value}
              onChange={(e) => onChange(e.target.value)}
              placeholder="自定义"
            />
          </Field>
        ) : null}
      </FieldSet>
    )
  }

  return (
    <Field>
      <FieldLabel htmlFor={question.slot}>
        {question.title}
        {question.required ? null : (
          <span className="text-xs font-normal text-muted-foreground">
            可跳过
          </span>
        )}
      </FieldLabel>
      {question.kind === "textarea" ? (
        <Textarea
          id={question.slot}
          value={value}
          rows={3}
          placeholder={question.placeholder}
          onChange={(e) => onChange(e.target.value)}
        />
      ) : (
        <Input
          id={question.slot}
          value={value}
          placeholder={question.placeholder}
          onChange={(e) => onChange(e.target.value)}
        />
      )}
      {question.help ? (
        <FieldDescription>{question.help}</FieldDescription>
      ) : null}
      {showSuggestion ? (
        <FieldDescription className="flex items-start gap-2">
          <Button
            type="button"
            variant="ghost"
            size="xs"
            onClick={() => onChange(suggested)}
          >
            <WandSparklesIcon data-icon="inline-start" />
            用视觉分析的建议
          </Button>
          <span className="flex-1 text-muted-foreground">{suggested}</span>
        </FieldDescription>
      ) : null}
    </Field>
  )
}
