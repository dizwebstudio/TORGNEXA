import {useMemo} from "react";

type Series={key:string;label:string;values:number[]};
export function AnalyticsChart({labels,series,format=(v)=>String(Math.round(v))}:{labels:string[];series:Series[];format?:(value:number)=>string}){
 const width=960,height=300,pad={l:56,r:24,t:24,b:48};const max=useMemo(()=>Math.max(1,...series.flatMap(s=>s.values)),[series]);const count=Math.max(labels.length,1);const x=(i:number)=>pad.l+(count<=1?0:(width-pad.l-pad.r)*i/(count-1));const y=(v:number)=>height-pad.b-(height-pad.t-pad.b)*(v/max);const ticks=[0,.25,.5,.75,1].map(v=>max*v);
 return <div className="analytics-chart"><div className="chart-legend">{series.map((s,i)=><span key={s.key}><i data-series={i}/>{s.label}</span>)}</div><svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label={`График: ${series.map(s=>s.label).join(", ")}`}>
  {ticks.map((v,i)=><g key={i}><line className="chart-grid-line" x1={pad.l} x2={width-pad.r} y1={y(v)} y2={y(v)}/><text className="chart-axis-label" x={pad.l-10} y={y(v)+4} textAnchor="end">{format(v)}</text></g>)}
  {series.map((s,si)=>{const points=s.values.map((v,i)=>`${x(i)},${y(v)}`).join(" ");return <g key={s.key} data-series={si}><polyline className="chart-line" points={points} fill="none"/>{s.values.map((v,i)=><circle className="chart-point" key={i} cx={x(i)} cy={y(v)} r="4"><title>{labels[i]} · {s.label}: {format(v)}</title></circle>)}</g>})}
  {labels.map((label,i)=>i%Math.max(1,Math.ceil(labels.length/8))===0?<text className="chart-axis-label" key={i} x={x(i)} y={height-18} textAnchor="middle">{label.slice(5)}</text>:null)}
 </svg></div>
}
