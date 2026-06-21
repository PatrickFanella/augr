import type { ISODate } from '@/shared/types/primitives'
import type { User } from '@/shared/types/domain'

export type LoginRequest = {
  username: string
  password: string
}

export type RefreshRequest = {
  refresh_token: string
}

export type AuthResponse = {
  access_token: string
  refresh_token: string
  expires_at: ISODate
}

export type AuthSession = {
  user: User
  access_token: string
  refresh_token: string
  expires_at: ISODate
}
