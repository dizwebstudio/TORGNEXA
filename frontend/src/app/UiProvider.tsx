import {createContext,useContext,useEffect,useMemo,useState} from "react";
import type {ReactNode} from "react";
import {ToastProvider} from "../components/Toast";

type Theme="light"|"dark";
type UIContext={theme:Theme;toggleTheme:()=>void;compact:boolean;toggleCompact:()=>void};
const Context=createContext<UIContext>({theme:"light",toggleTheme:()=>undefined,compact:false,toggleCompact:()=>undefined});
export function UiProvider({children}:{children:ReactNode}){const [theme,setTheme]=useState<Theme>(()=>window.matchMedia?.("(prefers-color-scheme: dark)").matches?"dark":"light");const [compact,setCompact]=useState(false);useEffect(()=>{document.documentElement.dataset.theme=theme;document.documentElement.dataset.density=compact?"compact":"comfortable"},[theme,compact]);const value=useMemo(()=>({theme,toggleTheme:()=>setTheme(v=>v==="light"?"dark":"light"),compact,toggleCompact:()=>setCompact(v=>!v)}),[theme,compact]);return <Context.Provider value={value}><ToastProvider>{children}</ToastProvider></Context.Provider>}
export function useUi(){return useContext(Context)}
