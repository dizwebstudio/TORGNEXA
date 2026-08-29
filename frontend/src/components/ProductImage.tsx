import {useEffect,useState} from "react";
import {useQuery} from "@tanstack/react-query";

const uploadContentPathPattern=/^\/api\/v1\/uploads\/upl_[0-9a-f]{32}\/content$/;

export type ProductImageClient={getUploadContent(input:{uploadId:string}):Promise<{body:unknown;headers:Headers}>};

function ImagePlaceholder({className,failed,retry}:{className:string;failed:boolean;retry?:()=>void}){
  return <div className={`image-placeholder${failed?" image-placeholder-failed":""}${className?` ${className}`:""}`} role={failed?"alert":"status"} aria-label={failed?"Изображение не загрузилось":"Загрузка изображения"}>
    <strong>{failed?"Изображение не загрузилось":"Загрузка изображения…"}</strong>
    {failed&&retry?<button type="button" className="button ghost" onClick={retry}>Повторить</button>:null}
  </div>;
}

export function ProductImage({api,src,alt,className=""}:{api:ProductImageClient;src:string;alt:string;className?:string}){
  const internal=uploadContentPathPattern.test(src);
  const [failed,setFailed]=useState(false),[retryToken,setRetryToken]=useState(0);
  const query=useQuery({queryKey:["upload-content",src],enabled:internal,staleTime:0,gcTime:0,queryFn:async()=>{
    const uploadId=src.split("/")[4];
    const response=await api.getUploadContent({uploadId});
    const contentType=response.headers.get("content-type")||"application/octet-stream";
    return URL.createObjectURL(new Blob([response.body as ArrayBuffer],{type:contentType}));
  }});
  useEffect(()=>()=>{if(query.data)URL.revokeObjectURL(query.data)},[query.data]);
  useEffect(()=>{setFailed(false);setRetryToken(0)},[src]);
  if(!internal){
    if(failed)return <ImagePlaceholder className={className} failed retry={()=>{setFailed(false);setRetryToken(token=>token+1)}}/>;
    return <img key={`${src}-${retryToken}`} className={className||undefined} src={src} alt={alt} loading="lazy" onError={()=>setFailed(true)} onLoad={()=>setFailed(false)}/>;
  }
  if(failed)return <ImagePlaceholder className={className} failed retry={()=>{setFailed(false);void query.refetch()}}/>;
  if(query.isError)return <ImagePlaceholder className={className} failed retry={()=>{setFailed(false);void query.refetch()}}/>;
  if(!query.data)return <ImagePlaceholder className={className} failed={false}/>;
  return <img className={className||undefined} src={query.data} alt={alt} loading="lazy" onError={()=>setFailed(true)} onLoad={()=>setFailed(false)}/>;
}
