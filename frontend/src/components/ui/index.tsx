import type {ButtonHTMLAttributes,InputHTMLAttributes,SelectHTMLAttributes,ReactNode} from 'react'
import { useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import {X} from 'lucide-react'
import {useToastStore} from '../../stores/useToastStore'
const control='rounded-md border border-zinc-200 bg-white px-3 py-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:border-zinc-700 dark:bg-zinc-900'
export function Button({className='',...p}:ButtonHTMLAttributes<HTMLButtonElement>){return <button className={`cursor-pointer disabled:cursor-not-allowed inline-flex items-center justify-center gap-1.5 rounded-md border border-zinc-200 bg-white px-3 py-1.5 text-sm font-medium hover:bg-zinc-50 disabled:opacity-50 focus-visible:ring-2 focus-visible:ring-indigo-500 dark:border-zinc-700 dark:bg-zinc-900 dark:hover:bg-zinc-800 ${className}`} {...p}/>}
export function Card({children,className=''}:{children:ReactNode;className?:string}){return <div className={`rounded-lg border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900 ${className}`}>{children}</div>}
export function Badge({children,className=''}:{children:ReactNode;className?:string}){return <span className={`inline-flex rounded bg-zinc-100 px-1.5 py-0.5 text-xs text-zinc-600 dark:bg-zinc-800 dark:text-zinc-300 ${className}`}>{children}</span>}
export function Input(p:InputHTMLAttributes<HTMLInputElement>){return <input {...p} className={`${control} ${p.className||''}`}/>}
export function Select(p:SelectHTMLAttributes<HTMLSelectElement>){return <select {...p} className={`${control} ${p.className||''}`}/>}
export function Switch({checked,onChange,label}:{checked:boolean;onChange:(v:boolean)=>void;label:string}){return <label className="inline-flex cursor-pointer items-center gap-2 text-sm"><button type="button" role="switch" aria-label={label} aria-checked={checked} onClick={()=>onChange(!checked)} className={`h-5 w-9 rounded-full p-0.5 ${checked?'bg-indigo-600':'bg-zinc-300 dark:bg-zinc-700'}`}><span className={`block h-4 w-4 rounded-full bg-white transition-transform ${checked?'translate-x-4':''}`}/></button>{label}</label>}
interface DialogProps {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
}

const dialogStack: { close: () => void; layer: number }[] = []

function closeTopDialog(event: KeyboardEvent) {
  const top = dialogStack.at(-1)
  if (event.key !== 'Escape' || event.repeat || event.defaultPrevented || !top) return
  event.preventDefault()
  top.close()
}

function useDialogLayer(open: boolean, onClose: () => void) {
  const close = useRef(onClose)
  const [layer, setLayer] = useState(50)

  useLayoutEffect(() => { close.current = onClose }, [onClose])
  useLayoutEffect(() => {
    if (!open) return
    const entry = { close: () => close.current(), layer: (dialogStack.at(-1)?.layer ?? 49) + 1 }
    if (!dialogStack.length) window.addEventListener('keydown', closeTopDialog)
    dialogStack.push(entry)
    setLayer(entry.layer)
    return () => {
      dialogStack.splice(dialogStack.indexOf(entry), 1)
      if (!dialogStack.length) window.removeEventListener('keydown', closeTopDialog)
    }
  }, [open])

  return layer
}

export function Modal({ open, onClose, title, children }: DialogProps) {
  const zIndex = useDialogLayer(open, onClose)
  if (!open) return null
  return createPortal(
    <div className="fixed inset-0 flex items-center justify-center bg-black/50 p-4" style={{ zIndex }} onClick={onClose}>
      <div role="dialog" aria-modal="true" aria-label={title} onClick={event => event.stopPropagation()} className="max-h-[85vh] w-full max-w-lg overflow-auto rounded-xl border border-zinc-200 bg-white p-5 text-zinc-900 shadow-xl dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="font-semibold">{title}</h2>
          <button type="button" className="cursor-pointer" onClick={onClose} aria-label="关闭"><X size={18}/></button>
        </div>
        {children}
      </div>
    </div>,
    document.body,
  )
}

export function ConfirmDialog({open,onClose,onConfirm,title,children,danger=false}:{open:boolean;onClose:()=>void;onConfirm:()=>void;title:string;children:ReactNode;danger?:boolean}){return <Modal open={open} onClose={onClose} title={title}><p className="text-sm text-zinc-500">{children}</p><div className="mt-5 flex justify-end gap-2"><Button onClick={onClose}>取消</Button><Button className={danger?'!border-red-600 !bg-red-600 !text-white':''} onClick={()=>{onConfirm();onClose()}}>确认</Button></div></Modal>}

export function Drawer({ open, onClose, title, children }: DialogProps) {
  const zIndex = useDialogLayer(open, onClose)
  if (!open) return null
  return createPortal(
    <div className="fixed inset-0 bg-black/50" style={{ zIndex }} onClick={onClose}>
      <div role="dialog" aria-modal="true" aria-label={title} onClick={event => event.stopPropagation()} className="absolute inset-x-0 bottom-0 rounded-t-xl bg-white p-5 pb-[max(1.25rem,env(safe-area-inset-bottom))] text-zinc-900 dark:bg-zinc-900 dark:text-zinc-100">
        <div className="mb-4 flex justify-between font-semibold">
          {title}
          <button type="button" className="cursor-pointer" onClick={onClose} aria-label="关闭"><X size={18}/></button>
        </div>
        {children}
      </div>
    </div>,
    document.body,
  )
}

export function Tabs({ value, onChange, items }: { value: string; onChange: (value: string) => void; items: { value: string; label: string }[] }) {
  const selected = items.findIndex(item => item.value === value)
  return <div role="tablist" className="flex flex-wrap gap-1 rounded-md bg-zinc-100 p-1 dark:bg-zinc-800">
    {items.map((item, index) => <button
      type="button"
      role="tab"
      aria-selected={value === item.value}
      tabIndex={index === Math.max(0, selected) ? 0 : -1}
      key={item.value}
      onClick={() => onChange(item.value)}
      onKeyDown={event => {
        if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
        event.preventDefault()
        const direction = event.key === 'ArrowLeft' ? -1 : 1
        const next = (index + direction + items.length) % items.length
        event.currentTarget.parentElement?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus()
      }}
      className={`cursor-pointer rounded px-2.5 py-1 text-xs ${value === item.value ? 'bg-white font-medium shadow-sm dark:bg-zinc-700' : 'text-zinc-500'}`}
    >{item.label}</button>)}
  </div>
}

export function ToastContainer(){const {items,remove}=useToastStore();return <div aria-live="polite" className="fixed right-4 top-4 z-[60] space-y-2">{items.map(t=><button key={t.id} onClick={()=>remove(t.id)} className={`cursor-pointer block rounded-md border bg-white px-4 py-3 text-sm shadow-lg dark:bg-zinc-900 ${t.kind==='error'?'border-red-400 text-red-600':t.kind==='success'?'border-emerald-400 text-emerald-600':'border-zinc-300'}`}>{t.message}</button>)}</div>}
export function EmptyState({children}:{children:ReactNode}){return <div className="py-8 text-center text-sm text-zinc-400">{children}</div>}
export function Skeleton({className=''}:{className?:string}){return <div className={`animate-pulse rounded bg-zinc-100 dark:bg-zinc-800 ${className}`}/>}
