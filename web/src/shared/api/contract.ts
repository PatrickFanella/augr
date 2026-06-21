import { z } from 'zod'

export class ApiContractError extends Error {
  readonly issues: z.ZodIssue[]

  constructor(label: string, issues: z.ZodIssue[]) {
    super(`API contract validation failed for ${label}: ${z.prettifyError(new z.ZodError(issues))}`)
    this.name = 'ApiContractError'
    this.issues = issues
  }
}

export function parseContract<T>(label: string, schema: z.ZodType<T>, payload: unknown): T {
  const result = schema.safeParse(payload)
  if (result.success) {
    return result.data
  }
  throw new ApiContractError(label, result.error.issues)
}

export const isoDateSchema = z.iso.datetime({ offset: true }).or(z.iso.datetime({ local: true }))
export const uuidSchema = z.uuid()
export const rawJsonSchema = z.unknown()
export const forwardCompatibleEnumSchema = z.string().min(1)

export function optionalNullable<T extends z.ZodType>(schema: T) {
  return schema.nullish().transform((value) => value ?? undefined)
}
