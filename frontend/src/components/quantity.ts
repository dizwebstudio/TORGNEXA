export function formatQuantityUnit(unit: string, quantity?: number): string {
  if (unit.trim().toUpperCase() !== "PCS") return unit;
  if (quantity === 1) return "штука";
  if (quantity !== undefined && quantity >= 2 && quantity <= 4) return "штуки";
  return "штук";
}
