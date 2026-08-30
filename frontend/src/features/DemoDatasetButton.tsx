import {useMutation,useQueryClient} from "@tanstack/react-query";
import {useApi} from "../api/ApiProvider";
import {useToast} from "../components/Toast";
import {refreshDemoDataset} from "./demoDataset";

export function DemoDatasetButton(){
  const api=useApi(),cache=useQueryClient(),toast=useToast();
  const seed=useMutation({
    mutationFn:()=>api.createDemoOrders({idempotencyKey:"demo-dataset:all"}),
    onSuccess:async()=>{
      await refreshDemoDataset(cache);
      toast.push({kind:"success",title:"Демо-данные добавлены",body:"Добавлены товары, заказы, остатки, финансы, интеграции, синхронизация, согласования и уведомления."});
    },
    onError:()=>toast.push({kind:"error",title:"Не удалось добавить демо-данные",body:"Проверьте права пользователя и повторите операцию."}),
  });
  return <button type="button" className="button primary" disabled={seed.isPending} onClick={()=>seed.mutate()}>{seed.isPending?"Добавляем демо-данные…":"Добавить демо-данные"}</button>;
}
