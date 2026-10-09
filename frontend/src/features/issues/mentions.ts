export interface MentionTrigger { sigil: "@" | "#"; query: string; /** Index of the sigil in the text. */ start: number }
export interface MentionOption { key: string; /** Text inserted in place of the trigger, without the trailing space. */ insert: string; label: string; hint?: string }
export interface IssueRef { number: number; title: string }

// The sigil must start the text or follow a non-word character, like GitHub's own parsing.
const TRIGGER = /(?:^|[^\w@/#&.])([@#])([\w-]*)$/;

/** The `@name` or `#12` being typed immediately before the caret, if any. */
export function activeTrigger(text: string, caret: number): MentionTrigger | null {
  const before = text.slice(0, caret);
  const match = TRIGGER.exec(before);
  if (!match) return null;
  const query = match[2]!;
  return { sigil: match[1] as "@" | "#", query, start: before.length - query.length - 1 };
}

/** People first by prefix, then by substring; issues by number prefix, then title substring. Empty queries list the newest. */
export function suggestMentions(trigger: MentionTrigger, people: string[], issues: IssueRef[], limit = 6): MentionOption[] {
  const query = trigger.query.toLowerCase();
  if (trigger.sigil === "@") {
    const unique = [...new Map(people.map((login) => [login.toLowerCase(), login])).values()];
    const prefix = unique.filter((login) => login.toLowerCase().startsWith(query));
    const inner = unique.filter((login) => !login.toLowerCase().startsWith(query) && login.toLowerCase().includes(query));
    return [...prefix, ...inner].slice(0, limit).map((login) => ({ key: `@${login}`, insert: `@${login}`, label: `@${login}` }));
  }
  const byNumber = issues.filter((issue) => String(issue.number).startsWith(query));
  const byTitle = issues.filter((issue) => !String(issue.number).startsWith(query) && issue.title.toLowerCase().includes(query));
  return [...byNumber, ...byTitle].slice(0, limit).map((issue) => ({ key: `#${issue.number}`, insert: `#${issue.number}`, label: `#${issue.number}`, hint: issue.title }));
}

/** Replaces the trigger with the chosen option and a trailing space; returns the new text and caret. */
export function applyMention(text: string, caret: number, trigger: MentionTrigger, option: MentionOption): { text: string; caret: number } {
  const insert = `${option.insert} `;
  return { text: text.slice(0, trigger.start) + insert + text.slice(caret), caret: trigger.start + insert.length };
}
