import type { Question, QuestionAnswer, QuestionRequest } from "@/mygo";

export interface QuestionDraft {
  choices: string[];
  text: string;
}

/** Matches the entire request snapshot, so a reused ID cannot activate stale history. */
export function questionRequestKey(request: QuestionRequest): string {
  return JSON.stringify([request.id, request.at, request.expiresAt ?? null, (request.questions ?? []).map((question) => [
    question.id, question.header ?? "", question.prompt, !!question.multiple, !!question.allowText, !!question.secret,
    (question.options ?? []).map((option) => [option.id, option.label, option.description ?? ""]),
  ])]);
}

export function validateQuestionAnswer(question: Question, draft: QuestionDraft): string | null {
  const known = new Set((question.options ?? []).map((option) => option.id));
  if (draft.choices.some((value) => !known.has(value))) return "An option is no longer available. Choose again.";
  if (new Set(draft.choices).size !== draft.choices.length) return "Choose each option only once.";
  if (draft.text && !question.allowText) return "This question accepts only the listed choices.";
  const count = draft.choices.length + (draft.text.trim() ? 1 : 0);
  if (count === 0) return question.allowText ? "Choose an option or enter an answer." : "Choose an option to continue.";
  if (!question.multiple && count !== 1) return "Choose one answer.";
  return null;
}

/** Returns one validated answer for every provider question; never partial submissions. */
export function questionAnswers(request: QuestionRequest, drafts: Record<string, QuestionDraft>): QuestionAnswer[] {
  if (!request.questions?.length) throw new Error("This request contains no questions.");
  const ids = new Set<string>();
  return request.questions.map((question) => {
    if (!question.id || ids.has(question.id)) throw new Error("The question request contains missing or duplicate IDs.");
    ids.add(question.id);
    const draft = Object.hasOwn(drafts, question.id) ? drafts[question.id]! : { choices: [], text: "" };
    const error = validateQuestionAnswer(question, draft);
    if (error) throw new Error(error);
    // Preserve literal text, especially secrets; trimming only determines whether it is empty.
    return { questionId: question.id, values: [...draft.choices, ...(draft.text.trim() ? [draft.text] : [])] };
  });
}
