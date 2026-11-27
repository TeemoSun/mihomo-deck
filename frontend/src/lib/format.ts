export function formatBytes(n:number) { if (!Number.isFinite(n)) return '—'; const units=['B','KB','MB','GB','TB','PB']; let i=0; while(Math.abs(n)>=1024 && i<units.length-1){n/=1024;i++} return `${n.toFixed(i ? 2 : 0)} ${units[i]}` }
export const formatSpeed=(n:number)=>`${formatBytes(n)}/s`
export function formatDuration(ms:number){const s=Math.max(0,Math.floor(ms/1000)); if(s<60)return `${s}秒`; if(s<3600)return `${Math.floor(s/60)}分${s%60}秒`; if(s<86400)return `${Math.floor(s/3600)}小时${Math.floor(s%3600/60)}分`; return `${Math.floor(s/86400)}天${Math.floor(s%86400/3600)}小时`}
export const formatUnixTime=(sec:number)=>sec ? new Date(sec*1000).toLocaleString('zh-CN') : '—'
export function formatRelative(sec:number){if(!sec)return '—'; const d=sec-Math.floor(Date.now()/1000); return d>=0 ? `${formatDuration(d*1000)}后` : `${formatDuration(-d*1000)}前`}
export function isInfoPseudoNode(name:string,pattern:string){try{return new RegExp(pattern || '(?!)','i').test(name)}catch{return /剩余流量|距离下次|套餐到期|官网|重置|到期|Traffic|Expire|Website/i.test(name)}}
export function delayTone(delay:number,alive?:boolean){return alive===false||delay<=0?'text-zinc-400':delay<200?'text-emerald-600 dark:text-emerald-400':delay<500?'text-amber-600 dark:text-amber-400':'text-red-500'}
