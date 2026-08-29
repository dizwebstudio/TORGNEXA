import {useEffect} from "react";
import {useQuery} from "@tanstack/react-query";

const uploadContentPathPattern=/^\/api\/v1\/uploads\/upl_[0-9a-f]{32}\/content$/;

export type ProductImageClient={getUploadContent(input:{uploadId:string}):Promise<{body:unknown;headers:Headers}>};

export function ProductImage({api,src,alt,className=""}:{api:ProductImageClient;src:string;alt:string;className?:string}){
  const internal=uploadContentPathPattern.test(src);
  const query=useQuery({queryKey:["upload-content",src],enabled:internal,staleTime:0,gcTime:0,queryFn:async()=>{
    const uploadId=src.split("/")[4];
    const response=await api.getUploadContent({uploadId});
    const contentType=response.headers.get("content-type")||"application/octet-stream";
    return URL.createObjectURL(new Blob([response.body as ArrayBuffer],{type:contentType}));
  }});
  useEffect(()=>()=>{if(query.data)URL.revokeObjectURL(query.data)},[query.data]);
  if(!internal)return <img className={className||undefined} src={src} alt={alt} loading="lazy"/>;
  if(!query.data)return <div className={`image-placeholder${className?` ${className}`:""}`} aria-label="Загрузка изображения"/>;
  return <img className={className||undefined} src={query.data} alt={alt} loading="lazy"/>;
}
