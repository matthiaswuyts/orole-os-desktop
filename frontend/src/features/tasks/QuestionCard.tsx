import { useEffect, useId, useRef, useState } from "react";
import { Agents, type QuestionAnswer, type QuestionRequest, type Session } from "@/mygo";
import { useAgents } from "@/stores/agents";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, CardTitle, CardDescription, CardPanel, CardFooter } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Field, FieldLabel } from "@/components/ui/field";
import { Fieldset, FieldsetLegend } from "@/components/ui/fieldset";
import { Input } from "@/components/ui/input";
import { Radio, RadioGroup } from "@/components/ui/radio-group";
import { useAction } from "@/features/agents/format";
import { questionAnswers, questionRequestKey, validateQuestionAnswer, type QuestionDraft } from "./questionState";

export function QuestionCard(props: { session: Session; request: QuestionRequest; answers?: QuestionAnswer[]; cancelled?: boolean }) {
  return <QuestionFlow key={`${props.session.id}:${questionRequestKey(props.request)}`} {...props} />;
}

function QuestionFlow({ session, request, answers, cancelled }: { session: Session; request: QuestionRequest; answers?: QuestionAnswer[]; cancelled?: boolean }) {
  const action = useAction();
  const titleId = useId();
  const promptId = useId();
  const errorId = useId();
  const fieldId = useId();
  const stepHeading = useRef<HTMLDivElement>(null);
  const focusStep = useRef(false);
  const [step, setStep] = useState(0);
  const [drafts, setDrafts] = useState<Record<string, QuestionDraft>>({});
  const [validation, setValidation] = useState("");
  const [receipt, setReceipt] = useState<{ answers: QuestionAnswer[]; cancelled: boolean } | null>(null);
  const [expired, setExpired] = useState(() => !!request.expiresAt && Date.parse(request.expiresAt) <= Date.now());
  const pending = session.questions?.find((item) => item.id === request.id);
  const active = session.live && !!pending && questionRequestKey(pending) === questionRequestKey(request) &&
    !expired && !receipt && answers === undefined && !cancelled;
  const question = request.questions?.[step];
  const draft = question && Object.hasOwn(drafts, question.id) ? drafts[question.id]! : { choices: [], text: "" };
  const confirmed = answers ?? receipt?.answers;
  const wasCancelled = cancelled ?? receipt?.cancelled;

  useEffect(() => {
    if (!request.expiresAt) return;
    const deadline = Date.parse(request.expiresAt);
    if (!Number.isFinite(deadline)) return;
    let timeout: number | undefined;
    const expire = () => {
      const remaining = deadline - Date.now();
      if (remaining <= 0) setExpired(true);
      else timeout = window.setTimeout(expire, Math.min(remaining, 2_147_483_647));
    };
    expire();
    return () => window.clearTimeout(timeout);
  }, [request.expiresAt]);

  useEffect(() => {
    if (!focusStep.current) return;
    stepHeading.current?.focus();
    focusStep.current = false;
  }, [step]);

  const changeStep = (next: number) => {
    focusStep.current = true;
    setStep(next);
    setValidation("");
    action.setError(null);
  };
  const submit = (cancel: boolean) => {
    if (!active || action.busy) return;
    if (request.expiresAt && Date.parse(request.expiresAt) <= Date.now()) { setExpired(true); return; }
    void action.run(async () => {
      const values = cancel ? [] : questionAnswers(request, drafts);
      const updated = await Agents.answerInput(session.id, request.id, values, cancel);
      setReceipt({ answers: values, cancelled: cancel });
      setDrafts({});
      useAgents.getState().upsert(updated);
    });
  };
  const next = () => {
    if (!question || !active || action.busy) return;
    const error = validateQuestionAnswer(question, draft);
    if (error) { setValidation(error); stepHeading.current?.focus(); return; }
    if (step < request.questions.length - 1) changeStep(step + 1);
    else submit(false);
  };

  return <Card aria-labelledby={titleId} className="min-w-0">
    <CardHeader>
      <CardTitle id={titleId}>Agent questions</CardTitle>
      <CardDescription>{active ? `Question ${step + 1} of ${request.questions?.length ?? 0}. All answers are sent together.` : wasCancelled ? "Question request cancelled." : confirmed ? "Answers submitted." : expired ? "This question request expired." : "This request is no longer pending. History is read-only."}</CardDescription>
    </CardHeader>
    <CardPanel>
      {active && question ? <form className="flex flex-col gap-4" onSubmit={(event) => { event.preventDefault(); next(); }}>
        <Fieldset className="flex flex-col gap-3" disabled={action.busy}>
          <FieldsetLegend>{question.header || `Question ${step + 1}`}</FieldsetLegend>
          <div id={promptId} ref={stepHeading} tabIndex={-1} className="whitespace-pre-wrap break-words text-sm focus-visible:outline-2 focus-visible:outline-ring">{question.prompt}</div>
          {question.multiple ? <div role="group" aria-labelledby={promptId} className="flex flex-col gap-1">
            {(question.options ?? []).map((option) => <label key={option.id} className="flex min-h-9 items-start gap-2 py-2 text-sm">
              <Checkbox value={option.id} checked={draft.choices.includes(option.id)} disabled={action.busy} onCheckedChange={(checked) => {
                setValidation("");
                setDrafts((previous) => ({ ...previous, [question.id]: { ...draft, choices: checked ? [...draft.choices, option.id] : draft.choices.filter((id) => id !== option.id) } }));
              }} />
              <span className="min-w-0 break-words">{option.label}{option.description && <span className="mt-1 block text-xs text-muted-foreground">{option.description}</span>}</span>
            </label>)}
          </div> : (question.options?.length ?? 0) > 0 && <RadioGroup aria-labelledby={promptId} value={draft.choices[0] ?? ""} disabled={action.busy} onValueChange={(value) => {
            if (typeof value !== "string") return;
            setValidation("");
            setDrafts((previous) => ({ ...previous, [question.id]: { choices: [value], text: "" } }));
          }}>
            {question.options.map((option) => <label key={option.id} className="flex min-h-9 items-start gap-2 py-2 text-sm">
              <Radio value={option.id} />
              <span className="min-w-0 break-words">{option.label}{option.description && <span className="mt-1 block text-xs text-muted-foreground">{option.description}</span>}</span>
            </label>)}
          </RadioGroup>}
          {question.allowText && <Field data-invalid={!!validation}>
            <FieldLabel htmlFor={fieldId}>{question.options?.length ? "Other answer" : "Your answer"}{question.secret ? " (secret)" : ""}</FieldLabel>
            <Input id={fieldId} type={question.secret ? "password" : "text"} autoComplete="off" spellCheck={!question.secret} value={draft.text} aria-invalid={!!validation} aria-describedby={validation ? errorId : undefined} disabled={action.busy} onChange={(event) => {
              setValidation("");
              setDrafts((previous) => ({ ...previous, [question.id]: { choices: question.multiple ? draft.choices : [], text: event.target.value } }));
            }} />
            {question.secret && <p className="text-xs text-muted-foreground">Sent to the agent provider when you submit. Hidden in the answer receipt.</p>}
          </Field>}
        </Fieldset>
        {validation && <p id={errorId} role="alert" className="text-xs text-destructive-foreground">{validation}</p>}
        <div className="flex flex-wrap items-center gap-2">
          {step > 0 && <Button type="button" size="sm" variant="outline" disabled={action.busy} onClick={() => changeStep(step - 1)}>Back</Button>}
          <Button type="submit" size="sm" loading={action.busy} disabled={action.busy}>{step < request.questions.length - 1 ? "Next" : "Submit answers"}</Button>
          <Button type="button" size="sm" variant="ghost" disabled={action.busy} onClick={() => submit(true)}>Cancel request</Button>
        </div>
      </form> : <dl className="flex flex-col gap-4 text-sm">
        {(request.questions ?? []).map((item) => {
          const values = confirmed?.find((answer) => answer.questionId === item.id)?.values;
          return <div key={item.id} className="flex flex-col gap-1">
            <dt className="whitespace-pre-wrap break-words">{item.prompt}</dt>
            <dd className="whitespace-pre-wrap break-words text-muted-foreground">{values?.length ? item.secret ? "Secret answer submitted" : values.map((value) => item.options?.find((option) => option.id === value)?.label ?? value).join(", ") : wasCancelled ? "Cancelled" : "No answer recorded"}</dd>
          </div>;
        })}
      </dl>}
    </CardPanel>
    {(action.error || receipt) && <CardFooter>
      {action.error ? <p role="alert" className="whitespace-pre-wrap break-words text-xs text-destructive-foreground">{action.error}</p> : <p role="status" className="text-xs text-muted-foreground">{receipt?.cancelled ? "Cancellation confirmed by the daemon." : "Answers accepted by the daemon."}</p>}
    </CardFooter>}
  </Card>;
}
