import {create} from 'zustand'
import {api} from '../api/client'
import type {AuthUser} from '../types/api'
interface State {user:AuthUser|null; csrfToken:string; checking:boolean; setCsrfToken:(s:string)=>void; login:(u:string,p:string)=>Promise<void>; checkAuth:()=>Promise<void>; logout:()=>Promise<void>; updatePassword:(a:string,b:string)=>Promise<void>}
export const useAuthStore=create<State>((set)=>({user:null,csrfToken:'',checking:true,setCsrfToken:(csrfToken)=>set({csrfToken}),login:async(u,p)=>{const user=await api.login(u,p);set({user,csrfToken:user.csrfToken})},checkAuth:async()=>{try{const user=await api.me();set({user,csrfToken:user.csrfToken})}catch{set({user:null,csrfToken:''})}finally{set({checking:false})}},logout:async()=>{try{await api.logout()}finally{set({user:null,csrfToken:''})}},updatePassword:async(a,b)=>{const result=await api.password(a,b);set(s=>({user:s.user?{...s.user,csrfToken:result.csrfToken}:null,csrfToken:result.csrfToken}))}}))
