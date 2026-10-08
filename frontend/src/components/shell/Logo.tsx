/** Orole OS mark: a lighthouse beam over a horizon line. */
export function Logo({ className = "size-5" }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M3.5 15.5h17" />
      <path d="M12 6.5v6.2" />
      <path d="M9.2 9.2 12 6.5l2.8 2.7" className="text-accent2" stroke="var(--accent-secondary)" />
    </svg>
  );
}
