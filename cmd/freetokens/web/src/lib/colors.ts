// A categorical palette for measured rows. The colors encode identity, not
// brand: every legend row repeats the label so color is never the only signal.
export const MODEL_COLORS = ["#c9a97b", "#34b27f", "#5b9dff", "#ee7458", "#efede8", "#9aa3a8", "#a78bfa", "#e07ab0"];
export const OTHER_COLOR = "#6f6a61";

export function modelColor(index: number): string {
  return MODEL_COLORS[index] ?? OTHER_COLOR;
}
