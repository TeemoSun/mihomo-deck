import {create} from 'zustand'
type Kind='success'|'error'|'info'; interface Toast {id:number;kind:Kind;message:string}
interface State {items:Toast[]; add:(kind:Kind,message:string)=>void; remove:(id:number)=>void;success:(s:string)=>void;error:(s:string)=>void;info:(s:string)=>void}
export const useToastStore=create<State>((set,get)=>({items:[],remove:(id)=>set(s=>({items:s.items.filter(x=>x.id!==id)})),add:(kind,message)=>{const id=Date.now()+Math.random();set(s=>({items:[...s.items,{id,kind,message}]}));setTimeout(()=>get().remove(id),4000)},success:s=>get().add('success',s),error:s=>get().add('error',s),info:s=>get().add('info',s)}))
