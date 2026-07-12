import type { ReactNode } from 'react'

export type TableProps = {
  children: ReactNode
  className?: string
}

export function TableWrap({ children, className }: TableProps) {
  return (
    <div className={`table-wrap ${className ?? ''}`}>
      {children}
    </div>
  )
}

export function Table({ children }: TableProps) {
  return <table className="w-full min-w-[760px] border-collapse">{children}</table>
}

export function Th({ children }: TableProps) {
  return <th>{children}</th>
}

export function Td({ children }: TableProps) {
  return <td className="max-w-[28rem] break-words">{children}</td>
}
