import type { ReactNode } from "react";
import type { ModelOption } from "@/mygo";
import { CompanyLogo, hasCompanyLogo } from "@/components/CompanyLogo";
import { ProviderLogo, hasProviderLogo } from "@/features/agents/ProviderLogo";
import { companyOf } from "@/lib/models";

/** The company behind a harness's own model aliases ("opus", "gpt-5"), which say nothing about it themselves. */
const HARNESS_COMPANY: Record<string, string> = { claude: "Anthropic", codex: "OpenAI", gemini: "Google", grok: "xAI" };

/** A harness's mark, or nothing for one without a bundled mark. */
export function harnessLogo(id: string): ReactNode {
  return hasProviderLogo(id) ? <ProviderLogo kind={id} /> : undefined;
}

/** The mark of the AI company behind a model, or nothing when it is unknown. */
export function modelLogo(harness: string, model: Pick<ModelOption, "id" | "name">): ReactNode {
  const company = HARNESS_COMPANY[harness] ?? companyOf(model);
  return hasCompanyLogo(company) ? <CompanyLogo company={company} className="size-4" /> : undefined;
}

/** A select label with its logo in front, for the closed value and the options alike. */
export function WithLogo({ logo, children }: { logo?: ReactNode; children: ReactNode }) {
  return <span className="flex min-w-0 items-center gap-2">{logo}<span className="truncate">{children}</span></span>;
}
