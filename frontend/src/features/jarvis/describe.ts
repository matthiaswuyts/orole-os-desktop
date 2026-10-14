import type { JarvisRule } from "@/mygo";

const short = (id: string) => id.slice(0, 8);

/** One rule in plain words, for the list on a bot's phone-calls page. */
export function describeRule(rule: JarvisRule): string {
  const any = rule.target === "any";
  let text: string;
  switch (rule.trigger) {
    case "task_finished":
    case "task_failed": {
      const what = rule.trigger === "task_failed" ? "fails" : "finishes";
      text = any ? `When any task I start ${what}` : `When task ${short(rule.target)} ${what}`;
      break;
    }
    case "automation_finished":
    case "automation_failed": {
      const what = rule.trigger === "automation_failed" ? "fails" : "succeeds";
      text = any ? `When any automation ${what}` : `When automation ${short(rule.target)} ${what}`;
      break;
    }
    case "notification": {
      const [kind, source] = [rule.target.split(":")[0], rule.target.split(":").slice(1).join(":")];
      text = `On ${kind} notifications${source ? ` from ${source}` : ""}`;
      break;
    }
    case "at_time":
      text = rule.repeat ? `Every day at ${rule.target}` : `At ${rule.target}`;
      break;
    default:
      text = rule.trigger;
  }
  if (!rule.repeat) text += " · once";
  if (rule.quietOverride) text += " · even in quiet hours";
  if (rule.urgent) text += " · urgent";
  if (rule.expiresAt) text += ` · until ${new Date(rule.expiresAt).toLocaleDateString(undefined, { dateStyle: "medium" })}`;
  return text;
}
