export function StatusBadge({value}: {value: string}) {
  return <span className={`status status-${value.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`}>{value}</span>;
}
