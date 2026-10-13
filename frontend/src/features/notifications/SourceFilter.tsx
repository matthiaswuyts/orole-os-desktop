import { Select, SelectGroup, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";

/**
 * The notification source picker. Loaded with the open popover rather than at startup: Base UI's Select is ~60 KB and
 * nothing else on the startup path uses it (see scripts/bundle-budget.json).
 */
export default function SourceFilter({ value, items, onChange }: { value: string; items: { value: string; label: string }[]; onChange: (value: string) => void }) {
  return <Select value={value} items={items} onValueChange={(next) => onChange(next ?? "")}>
    <SelectTrigger size="sm" aria-label="Notification source" className="w-auto min-w-0 max-w-24 border-transparent bg-transparent text-muted-foreground shadow-none"><SelectValue /></SelectTrigger>
    <SelectPopup><SelectGroup>{items.map((source) => <SelectItem key={source.value} value={source.value}>{source.label}</SelectItem>)}</SelectGroup></SelectPopup>
  </Select>;
}
