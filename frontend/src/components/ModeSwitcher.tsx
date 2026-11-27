import {useState} from 'react'
import {api} from '../api/client'
import {useToastStore} from '../stores/useToastStore'
import {ConfirmDialog,Tabs} from './ui'
export default function ModeSwitcher({mode,onChange}:{mode:string;onChange:(s:string)=>void}){const [confirm,setConfirm]=useState(false);const toast=useToastStore();const change=(v:string)=>{void api.patchConfig({mode:v}).then(()=>{onChange(v);window.dispatchEvent(new Event('deck-mode'));toast.success('模式已切换')}).catch(()=>toast.error('切换模式失败'))};return <><Tabs value={mode.toLowerCase()} onChange={v=>v==='direct'?setConfirm(true):change(v)} items={[{value:'rule',label:'规则'},{value:'global',label:'全局'},{value:'direct',label:'直连'}]}/><ConfirmDialog open={confirm} onClose={()=>setConfirm(false)} onConfirm={()=>change('direct')} title="切换为直连模式" danger>切换后流量将直接连接，确定继续吗？</ConfirmDialog></>}
