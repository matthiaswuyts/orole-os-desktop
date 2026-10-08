import { type ClassValue, clsx } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

// The density sizes in index.css (h-row, py-row-pad, gap-row-gap,
// h-side-row) are spacing like h-8, so they replace it when merged.
const twMerge = extendTailwindMerge({ extend: { theme: { spacing: ["row", "row-pad", "row-gap", "side-row"] } } });

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
