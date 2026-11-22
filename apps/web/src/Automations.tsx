import { defaultCopy } from "@humagen/components";
import { CalendarIcon, GitHubIcon, SlackIcon } from "@humagen/render-web";
import { useState } from "react";
import type { Revision, ScheduleView } from "./api.ts";
import { AgentTag, ItemCard, itemGrid, Pill, Tag } from "./cards.tsx";
import { inWords } from "./cron.ts";
import type { ProjectRef } from "./projects.ts";
import { href } from "./route.ts";
import { ScheduleForm } from "./Schedules.tsx";
import { ago, Empty, Page, primary } from "./ui.tsx";

const words = defaultCopy.automations;
const sends = defaultCopy.schedules;

/**
 * Automations: work an agent starts without being asked, and where each
 * answer goes. What starts one is its trigger; a schedule is the one trigger
 * there is. Each run is a session. A GitHub request is not an automation: it
 * starts its own session, which answers where it was asked. Background work
 * (watches, goals and tasks) belongs to the session that carries it and is
 * listed in that session's Details, not here.
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
                // A real link, so a modified click opens the automation in a new tab.
                href={href({ page: "schedules", name: s.name })}
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
