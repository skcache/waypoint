"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { ArrowDown } from "@phosphor-icons/react";

import type { DemoExport } from "@/lib/demo";
import { cleanEffect, orderIdentities, receiptModel } from "@/lib/evidence";
import { humanStatus, money, titleFor } from "@/lib/showcase";
import { Receipt, ROW_INVARIANTS, type RowKey } from "./receipt";
import { Reveal } from "./kit";
import type { EvidenceSource } from "./types";

const WORDS: Record<number, string> = { 1: "One", 2: "Two", 3: "Three", 4: "Four" };
const word = (n: number | null) => (n === null ? "More" : WORDS[n] ?? String(n));


export function BreakScene({ doc, source }: { doc: DemoExport; source: EvidenceSource }) {
  const model = useMemo(() => receiptModel(doc, "before"), [doc]);
  const amount = money(doc.incident.total_minor, doc.incident.currency);
  const ids = orderIdentities(doc);
  const [step, setStep] = useState(0);
  const [picked, setPicked] = useState<RowKey | null>(null);
  const stepRefs = useRef<(HTMLElement | null)[]>([]);
  const webhookRows = doc.incident.observations.some((o) => /webhook/i.test(o.method));
  const caseLabel = `Case file ${doc.incident.fault} · ${titleFor(doc)}`;

  useEffect(() => {
    const els = stepRefs.current.filter(Boolean) as HTMLElement[];
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) if (e.isIntersecting) setStep(Number((e.target as HTMLElement).dataset.step));
      },
      { rootMargin: "-42% 0px -42% 0px" },
    );
    els.forEach((el) => io.observe(el));
    return () => io.disconnect();
  }, []);

  const effectKeys = model.effects.map((_, i) => `effect-${i + 1}` as RowKey);
  const byStep: RowKey[][] = [[], ["capture"], effectKeys, ["status"], ["totals"]];
  const highlight = picked ? [picked] : byStep[step] ?? [];
  const stamped = step >= 4 || picked !== null;
  const picks = picked ? ROW_INVARIANTS[picked] : [];
  const picksInv = doc.pre_repair.invariants.filter((i) => picks.includes(i.id));
  const fulfilled = model.fulfilled;

  const beats = [
    {
      title: "Captured once.",
      body: `The provider reported a single capture${model.capture ? `, ${model.capture}` : ""}, for ${amount}.`,
    },
    {
      title: `Fulfilled ${fulfilled === 2 ? "twice" : `${fulfilled ?? "many"} times`}.`,
      body: `The merchant's ledger holds ${fulfilled ?? "more than one"} fulfillment effects${model.effects.length ? `, ${model.effects.map((e) => e.key).join(" and ")},` : ""} for that one payment.`,
    },
    {
      title: "And the order forgot.",
      body: `Its final status is ${humanStatus(model.status).toLowerCase()}: after a verified capture, the order rolled back.`,
    },
    {
      title: "A separate judge says no.",
      body: `It re-read the ledger and flagged ${model.failed.join(", ")}. Select any line on the receipt to see why. Then see what the AI did about it.`,
    },
  ];

  const note = (testId: string) => (
    <div className="mt-5 min-h-[104px] w-full max-w-[380px]" aria-live="polite" data-testid={testId}>
      {picksInv.length ? (
        picksInv.map((inv) => (
          <p key={inv.id} className="text-[16px] leading-snug">
            <b className="font-bold">
              {inv.id} · {inv.status === "FAIL" ? "fails" : "holds"}.
            </b>{" "}
            {inv.title ?? inv.summary}
            {inv.counterexample?.effects ? <span className="mono"> ({inv.counterexample.effects.map(cleanEffect).join(", ")})</span> : null}
          </p>
        ))
      ) : (
        <p className="text-[16px] leading-snug text-muted">Select a line on the receipt to see which invariant it breaks.</p>
      )}
    </div>
  );

  const receipt = (testId: string, className = "") => (
    <Receipt
      model={model}
      amount={amount}
      caseLabel={caseLabel}
      lens={1}
      highlight={highlight}
      stamped={stamped}
      selected={picked}
      onRow={(k) => setPicked((p) => (p === k ? null : k))}
      testId={testId}
      className={className}
    />
  );

  return (
    <section id="break" className="field-alarm relative" data-scene="break">
      <div className="mx-auto grid w-full max-w-[1440px] gap-x-10 px-5 sm:px-8 lg:grid-cols-[minmax(0,1fr)_400px] lg:px-12 xl:grid-cols-[minmax(0,1fr)_420px]">
        <div>
          <div ref={(el) => { stepRefs.current[0] = el; }} data-step={0} className="flex min-h-[calc(100svh-56px)] flex-col justify-between pb-10 pt-8 lg:pb-14 lg:pt-12">
            <p className="max-w-[34ch] text-[17px] font-medium leading-snug">
              An AI fixed a payment bug. A separate judge checked the ledger. This is the {source === "recorded" ? "recorded" : "local"} case.
            </p>
            <div>
              <h1 className="poster text-[clamp(3.4rem,8vw,8.4rem)]">
                One payment.
                <br />
                {word(fulfilled)} fulfillments.
              </h1>
              <p className="mt-8 max-w-[36ch] text-[clamp(1.2rem,2vw,1.6rem)] font-medium leading-[1.3]">
                {amount} captured once. The merchant&rsquo;s own ledger says it was fulfilled {fulfilled === 2 ? "twice" : "more than once"}.
              </p>
              <a href="#beat-1" className="mt-8 inline-flex h-14 items-center gap-3 bg-night px-6 text-[17px] font-semibold text-alarm transition-transform hover:-translate-y-0.5">
                Follow the money
                <ArrowDown size={18} weight="bold" aria-hidden />
              </a>
            </div>
          </div>

          <div className="mx-auto my-10 flex flex-col items-center lg:hidden">
            {receipt("receipt-incident-m")}
            {note("receipt-note-m")}
          </div>

          {beats.map((b, i) => (
            <div
              key={b.title}
              ref={(el) => { stepRefs.current[i + 1] = el; }}
              data-step={i + 1}
              id={i === 0 ? "beat-1" : undefined}
              className={`flex min-h-[46svh] items-center py-8 lg:min-h-[64svh] lg:py-10 transition-opacity duration-500 ${step === i + 1 ? "opacity-100" : "lg:opacity-30"}`}
            >
              <Reveal>
                <h2 className="poster text-[clamp(3rem,7.4vw,7rem)]">{b.title}</h2>
                <p className="mt-6 max-w-[34ch] text-[clamp(1.15rem,1.8vw,1.45rem)] font-medium leading-[1.35]">{b.body}</p>
              </Reveal>
            </div>
          ))}

          <div className="pb-24 pt-6">
            {!webhookRows ? (
              <details className="max-w-[560px] border-y-2 border-night">
                <summary className="flex min-h-14 items-center text-[17px] font-semibold">What this evidence cannot show</summary>
                <p className="pb-5 text-[17px] leading-[1.5]">
                  The export has no webhook-delivery rows, and its {doc.incident.observations.length} provider calls belong to the later verification order,
                  not this incident. How the second effect arrived, and when, was not observed. The AI&rsquo;s explanation in the next section is a
                  hypothesis about that, not a recording of it.
                </p>
              </details>
            ) : null}
          </div>
        </div>

        <aside className="relative max-lg:hidden">
          <div className="sticky top-[64px] flex flex-col items-center pb-8 pt-6">
            <Reveal kind="print">{receipt("receipt-incident")}</Reveal>
            {note("receipt-note")}
          </div>
        </aside>
      </div>
      <p className="sr-only">Incident order {ids.incident}</p>
    </section>
  );
}
