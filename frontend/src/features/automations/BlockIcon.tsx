import {
  Bell, Blocks, Bot, Code, FlaskConical, GitPullRequest, Hourglass, MessageSquareText, Plug, ShieldCheck, Split, SquareTerminal, Timer, Webhook,
  type LucideIcon,
} from "lucide-react";

const ICONS: Record<string, LucideIcon> = {
  "bot.tell": Bot, "task.start": Code, "task.prompt": MessageSquareText, "task.wait": Hourglass, notify: Bell, "http.request": Webhook,
  "github.query": GitPullRequest, "mcp.tool": Plug, condition: Split, delay: Timer, approval: ShieldCheck, "command.run": SquareTerminal, "test.run": FlaskConical,
};

export function BlockIcon({ type, className }: { type: string; className?: string }) {
  const Icon = ICONS[type] ?? Blocks;
  return <Icon aria-hidden className={className} />;
}
