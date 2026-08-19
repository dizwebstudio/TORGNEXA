import type {IconName} from "./icon-names";
export type {IconName} from "./icon-names";

const paths: Record<IconName, string[]> = {
  dashboard:["M3 3h7v7H3z","M14 3h7v4h-7z","M14 11h7v10h-7z","M3 14h7v7H3z"],
  catalog:["M4 7.5 12 3l8 4.5v9L12 21l-8-4.5z","M4 7.5 12 12l8-4.5","M12 12v9"],
  orders:["M6 3h12l2 4H4z","M5 7v13h14V7","M9 11h6"],
  inventory:["M3 6l9-3 9 3-9 3z","M3 6v12l9 3 9-3V6","M12 9v12","M7 12h2"],
  connectors:["M8 12h8","M6 8v8","M18 8v8","M3 9h3V6","M18 6v3h3","M3 15h3v3","M18 18v-3h3"],
  sync:["M20 7h-5V2","M4 17h5v5","M18.5 12a7 7 0 0 0-12-5L4 9","M5.5 12a7 7 0 0 0 12 5L20 15"],
  approvals:["M12 3l8 4v5c0 4.6-3.2 7.5-8 9-4.8-1.5-8-4.4-8-9V7z","m8 12 2.5 2.5L16 9"],
  compliance:["M6 3h9l3 3v15H6z","M15 3v4h4","M9 12h6","M9 16h4"],
  notifications:["M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9","M10 21h4"],
  reports:["M4 20V10","M10 20V4","M16 20v-7","M22 20H2"],
  audit:["M5 4h14v16H5z","M8 8h8","M8 12h8","M8 16h5"],
  settings:["M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7","M19.4 15a1.7 1.7 0 0 0 .34 1.88l.06.06-2.1 3.64-.08-.03a1.7 1.7 0 0 0-1.8.23l-.06.05-1.1-.64a1.7 1.7 0 0 0-1.6 0l-1.1.64-.06-.05a1.7 1.7 0 0 0-1.8-.23l-.08.03-2.1-3.64.06-.06A1.7 1.7 0 0 0 4.6 15v-1.28a1.7 1.7 0 0 0-.86-1.48L3.67 12l2.1-3.64.08.03a1.7 1.7 0 0 0 1.8-.23l.06-.05 1.1.64a1.7 1.7 0 0 0 1.6 0l1.1-.64.06.05a1.7 1.7 0 0 0 1.8.23l.08-.03 2.1 3.64-.06.06a1.7 1.7 0 0 0-.86 1.48z"],
  search:["M21 21l-4.3-4.3","M11 18a7 7 0 1 1 0-14 7 7 0 0 1 0 14"],
  command:["M9 6V5a3 3 0 1 0-3 3h12a3 3 0 1 0-3-3v14a3 3 0 1 0 3-3H6a3 3 0 1 0 3 3z"],
  moon:["M20 15.5A8 8 0 0 1 8.5 4 8 8 0 1 0 20 15.5z"],
  sun:["M12 17a5 5 0 1 0 0-10 5 5 0 0 0 0 10","M12 1v2","M12 21v2","M4.22 4.22l1.42 1.42","M18.36 18.36l1.42 1.42","M1 12h2","M21 12h2","M4.22 19.78l1.42-1.42","M18.36 5.64l1.42-1.42"],
  logout:["M10 17l5-5-5-5","M15 12H3","M13 3h7v18h-7"],
  chevron:["m9 18 6-6-6-6"], close:["M6 6l12 12","M18 6 6 18"], check:["m5 12 4 4L19 6"],
  warning:["M12 3 2 21h20z","M12 9v5","M12 18h.01"], error:["M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18","M9 9l6 6","M15 9l-6 6"],
  info:["M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18","M12 11v6","M12 7h.01"],
  activity:["M3 12h4l2-6 4 12 2-6h6"], filter:["M4 5h16l-6 7v6l-4 2v-8z"], sort:["M8 7h8","M8 12h6","M8 17h4"], columns:["M4 4h16v16H4z","M10 4v16","M15 4v16"], more:["M5 12h.01","M12 12h.01","M19 12h.01"], refresh:["M20 6v5h-5","M4 18v-5h5","M18.5 10A7 7 0 0 0 6 7.5L4 11","M5.5 14A7 7 0 0 0 18 16.5l2-3.5"],
  warehouse:["M3 9 12 4l9 5v11H3z","M8 20v-6h8v6","M7 10h2","M15 10h2"], incident:["M12 2l10 19H2z","M12 8v5","M12 17h.01"], clock:["M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18","M12 7v5l3 2"], arrowRight:["M5 12h14","m14 12-5-5","m14 12-5 5"], menu:["M4 7h16","M4 12h16","M4 17h16"]
};

export function Icon({name,size=18,className=""}:{name:IconName;size?:number;className?:string}) {
  return <svg className={`icon ${className}`} width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[name].map((d,index)=><path key={`${name}-${index}`} d={d}/>)}</svg>;
}
