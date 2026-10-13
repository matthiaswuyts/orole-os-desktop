import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { cn } from "@/lib/utils";

/** Stable hue per login so the same person is recognisable; initials only, no remote avatar request. */
function hue(login: string): number {
  let h = 0;
  for (const ch of login) h = (h * 31 + ch.charCodeAt(0)) % 360;
  return h;
}

export function PersonAvatar({ login, className }: { login: string; className?: string }) {
  return <Avatar className={cn("size-6 text-[11px]", className)}>
    <AvatarFallback style={{ background: `hsl(${hue(login)} 45% 38%)`, color: "white" }}>{login.slice(0, 1).toUpperCase()}</AvatarFallback>
  </Avatar>;
}
