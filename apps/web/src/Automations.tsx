import { defaultCopy } from "@humagen/components";
import { CalendarIcon, GitHubIcon, SlackIcon } from "@humagen/render-web";
import { useState } from "react";
import type { Revision, ScheduleView } from "./api.ts";
import { AgentTag, ItemCard, itemGrid, Pill, Tag } from "./cards.tsx";
import { inWords } from "./cron.ts";
import type { ProjectRef } from "./projects.ts";
import { ScheduleForm } from "./Schedules.tsx";
import { ago, Empty, Page, primary } from "./ui.tsx";

const words = defaultCopy.automations;
const sends = defaultCopy.schedules;

/**
 * Scheduled work: what runs at set times, and where each answer goes. Each run
 * is a session. A GitHub request is not scheduled: it starts its own session,
 * which answers where it was asked.
 */
export function Automations({
  schedules,
  agents,
  projects,
  can,
  onChanged,
  onSchedule,
}: {
  schedules: ScheduleView[];
  agents: Revision[];
  projects: ProjectRef[];
  can: (permission: string) => boolean;
  onChanged: () => void;
  onSchedule: (name: string) => void;
}) {
  const [making, setMaking] = useState(false);
  const canSchedule = can("schedules:write");
  return (
    <Page
      title={words.title}
      lede={words.lede}
      actions={
        !making &&
        canSchedule && (
          <button className={primary} type="button" onClick={() => setMaking(true)}>
            {words.newSchedule}
          </button>
        )
      }
    >
      {making && (
        <ScheduleForm
          agents={agents}
          projects={projects}
          onClose={() => setMaking(false)}
          onSaved={() => {
            setMaking(false);
            onChanged();
          }}
        />
      )}
      {schedules.length === 0 && !making ? (
        <Empty title={words.empty} />
      ) : (
        <ul className={itemGrid} aria-label={words.title}>
          {schedules.map((s) => (
            <li key={s.name} className="min-w-0">
              <ItemCard
                title={s.name}
                onOpen={() => onSchedule(s.name)}
                time={s.recent[0] ? ago(s.recent[0].created_at) : undefined}
                facts={
                  <>
                    <Pill tone={s.paused ? "neutral" : "ok"}>
                      {s.paused ? words.paused : words.active}
                    </Pill>
                    <Tag icon={<CalendarIcon size={12} />}>{inWords(s.expression)}</Tag>
                    <AgentTag name={s.agent} />
                    {s.deliver && (
                      <Tag
                        icon={
                          s.deliver === "slack" ? (
                            <SlackIcon size={12} />
                          ) : s.deliver === "github" ? (
                            <GitHubIcon size={12} />
                          ) : undefined
                        }
                      >
                        {sends.sentTo(s.deliver, s.deliver_to)}
                      </Tag>
                    )}
                  </>
                }
              />
            </li>
          ))}
        </ul>
      )}
    </Page>
  );
}
