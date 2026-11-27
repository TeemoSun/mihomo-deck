import {create} from 'zustand'
import {api} from '../api/client'
import type {Preferences} from '../types/api'
interface State {prefs:Preferences|null;loading:boolean;loadPrefs:()=>Promise<void>;savePrefs:(p:Partial<Preferences>)=>Promise<void>;applyTheme:(t:Preferences['theme'])=>void}
export const usePrefsStore=create<State>((set,get)=>({prefs:null,loading:false,applyTheme:(t)=>{localStorage.setItem('mihomo-deck-theme',t);document.documentElement.classList.toggle('dark',t==='dark'||(t==='system'&&matchMedia('(prefers-color-scheme: dark)').matches))},loadPrefs:async()=>{set({loading:true});try{const prefs=await api.preferences();set({prefs});get().applyTheme(prefs.theme)}finally{set({loading:false})}},savePrefs:async(p)=>{const prefs=await api.savePreferences(p);set({prefs});get().applyTheme(prefs.theme)}}))
