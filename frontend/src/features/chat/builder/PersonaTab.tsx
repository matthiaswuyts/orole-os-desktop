import { useState, type Dispatch } from "react";
import { Check, ChevronDown } from "lucide-react";
import { BotAvatar } from "@/components/shell/BotAvatar";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Menu, MenuItem, MenuPopup, MenuTrigger } from "@/components/ui/menu";
import { Textarea } from "@/components/ui/textarea";
import { ConfirmDialog } from "@/features/projects/dialogs";
import { BOT_COLORS, botTone } from "@/lib/botColors";
import { cn } from "@/lib/utils";
import { LIMITS, type Action, type FormState, type Problems } from "./botForm";
import { Block, Section } from "./controls";
import { TEMPLATES, type InstructionTemplate } from "./templates";

const count = (text: string) => Array.from(text).length;

export function PersonaTab({ form, dispatch, problems }: { form: FormState; dispatch: Dispatch<Action>; problems: Problems }) {
  const [pending, setPending] = useState<InstructionTemplate | null>(null);
  const patch = (p: Partial<FormState>) => dispatch({ type: "patch", patch: p });
  const apply = (template: InstructionTemplate) => patch({ instructions: template.instructions });
  const instructionChars = count(form.instructions);

  return (
    <div className="flex flex-col gap-3">
      <Section title="Identity" description="How this bot appears in your bot list and conversation.">
        <Block label="Name" error={problems.name}>
          {(id, describedBy) => (
            <Input nativeInput id={id} value={form.name} maxLength={LIMITS.name * 2} placeholder="Travel planner" autoComplete="off" aria-required="true" aria-invalid={!!problems.name || undefined} aria-describedby={describedBy} onChange={(e) => patch({ name: e.target.value })} />
          )}
        </Block>
        <div className="flex flex-wrap items-start gap-4">
          <Block label="Avatar" className="w-40" hint="One emoji or two letters. Empty uses the name’s initials." error={problems.avatar}>
            {(id, describedBy) => (
              <div className="flex items-center gap-2">
                <BotAvatar name={form.name} avatar={form.avatar} color={form.color} className="size-8 text-sm" />
                <Input nativeInput id={id} value={form.avatar} placeholder="🧭" autoComplete="off" aria-invalid={!!problems.avatar || undefined} aria-describedby={describedBy} onChange={(e) => patch({ avatar: e.target.value })} />
              </div>
            )}
          </Block>
          <fieldset className="flex min-w-0 flex-col gap-1.5">
            <legend className="mb-1.5 font-medium text-sm">Colour</legend>
            <div role="radiogroup" aria-label="Colour" className="flex flex-wrap gap-1.5">
              {BOT_COLORS.map((color) => (
                <button
                  key={color} type="button" role="radio" aria-checked={form.color === color} aria-label={color}
                  onClick={() => patch({ color })}
                  className={cn("relative grid size-7 place-items-center rounded-full outline-none ring-offset-background focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1", botTone(color), form.color === color && "ring-2 ring-foreground/60 ring-offset-1")}
                >
                  {form.color === color && <Check className="size-3.5" aria-hidden="true" />}
                </button>
              ))}
            </div>
          </fieldset>
        </div>
        <Block
          label="Description" error={problems.description}
          hint="Shown on the bot’s empty conversation, so you remember what it is for."
          aside={<span className={cn("text-xs tabular-nums", count(form.description) > LIMITS.description ? "text-destructive-foreground" : "text-muted-foreground")}>{count(form.description)} / {LIMITS.description}</span>}
        >
          {(id, describedBy) => (
            <Textarea id={id} size="sm" value={form.description} placeholder="Helps me plan trips and compare options." aria-invalid={!!problems.description || undefined} aria-describedby={describedBy} onChange={(e) => patch({ description: e.target.value })} />
          )}
        </Block>
      </Section>

      <Section title="Instructions" description="What the bot should always do. Sent with every message, so keep it focused.">
        <div className="flex items-center justify-between gap-2">
          <Menu>
            <MenuTrigger render={<Button type="button" size="xs" variant="outline" />}>Start from a template<ChevronDown /></MenuTrigger>
            <MenuPopup align="start" className="w-72">
              {TEMPLATES.map((template) => (
                <MenuItem key={template.id} onClick={() => (form.instructions.trim() && form.instructions !== template.instructions ? setPending(template) : apply(template))}>
                  <span className="flex min-w-0 flex-col">
                    <span className="font-medium">{template.name}</span>
                    <span className="text-muted-foreground text-xs">{template.summary}</span>
                  </span>
                </MenuItem>
              ))}
            </MenuPopup>
          </Menu>
        </div>
        <Block
          label="Instructions" error={problems.instructions}
          aside={<span aria-live="polite" className={cn("text-xs tabular-nums", instructionChars > LIMITS.instructions ? "text-destructive-foreground" : "text-muted-foreground")}>{instructionChars.toLocaleString("en-US")} / {LIMITS.instructions.toLocaleString("en-US")}</span>}
          hint="Templates only fill this box. Edit them freely; nothing is created until you save."
        >
          {(id, describedBy) => (
            <Textarea id={id} value={form.instructions} className="[&_textarea]:min-h-48" placeholder="You are a travel planner. Ask where and when, then …" aria-invalid={!!problems.instructions || undefined} aria-describedby={describedBy} onChange={(e) => patch({ instructions: e.target.value })} />
          )}
        </Block>
      </Section>

      <ConfirmDialog
        open={pending !== null} onOpenChange={(o) => { if (!o) setPending(null); }}
        title={`Replace the instructions with “${pending?.name ?? ""}”?`} confirmLabel="Replace instructions"
        onConfirm={async () => { if (pending) apply(pending); }}
      >
        <p>The text in the instructions box will be replaced. You can still edit it before saving.</p>
      </ConfirmDialog>
    </div>
  );
}
