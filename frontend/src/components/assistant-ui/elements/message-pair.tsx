/*
 * Adapted from assistant-ui MessagePair (runtime composition: MessagePrimitive.Root per message and an
 * ActionBarPrimitive row), commit 9201447608271c64a8a9ca66c69e81d27a1d9fab.
 * See docs/research/upstream-attribution.md.
 * MIT License — Copyright (c) 2026 AgentbaseAI Inc.
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */
import { ActionBarPrimitive, MessagePrimitive, useAui } from "@assistant-ui/react";
import { RefreshCwIcon } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/**
 * One message of a conversation. `MessagePrimitive.Root` tracks hover for the action row; keyboard focus inside
 * the message counts as hover so the row is reachable without a pointer.
 */
export function MessagePair({ className, onFocus, onBlur, ...props }: ComponentProps<typeof MessagePrimitive.Root>) {
  const aui = useAui();
  return <MessagePrimitive.Root data-slot="message-pair" className={cn("group/message flex w-full min-w-0 flex-col", className)}
    onFocus={(event) => { onFocus?.(event); aui.message.setIsHovering(true); }}
    onBlur={(event) => {
      onBlur?.(event);
      if (!event.currentTarget.contains(event.relatedTarget)) aui.message.setIsHovering(false);
    }}
    {...props} />;
}

/**
 * The hover actions under a reply. `autohide="not-last"` keeps the newest reply's actions visible and hides the
 * others until hover or focus. Regenerate is only offered when the caller can honour it (the last reply).
 */
export function MessageActions({ regenerate, children, className }: { regenerate?: boolean; children?: ReactNode; className?: string }) {
  return <ActionBarPrimitive.Root autohide="not-last" data-slot="message-actions" className={cn("flex items-center gap-0.5 text-muted-foreground", className)}>
    {children}
    {regenerate && <ActionBarPrimitive.Reload asChild>
      <Button type="button" variant="ghost" size="icon-xs" aria-label="Regenerate response"><RefreshCwIcon aria-hidden /></Button>
    </ActionBarPrimitive.Reload>}
  </ActionBarPrimitive.Root>;
}
