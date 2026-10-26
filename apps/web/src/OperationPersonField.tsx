import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { api, type Person } from "./api.ts";
import { field, Label } from "./ui.tsx";

const words = defaultCopy.operations;
/** Names and handles are visible; provider subjects remain form values only. */
export function OperationPersonField({
  label,
  value,
  displayName,
  disabled,
  onChange,
}: {
  label: string;
  value: string;
  displayName?: string;
  disabled?: boolean;
  onChange: (value: string) => void;
}) {
  const [query, setQuery] = useState("");
  const [people, setPeople] = useState<Person[]>([]);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let live = true;
    const timer = setTimeout(() => {
      api
        .people({ q: query, limit: 50 })
        .then((result) => {
          if (live) {
            setPeople(result.people);
            setFailed(false);
          }
        })
        .catch(() => {
          if (live) setFailed(true);
        });
    }, 150);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [query]);
  const current = people.some((person) => (person.subject ?? `@${person.handle}`) === value);
  return (
    <div className="flex flex-col gap-2">
      <Label text={label}>
        <select
          className={field}
          value={value}
          disabled={disabled}
          onChange={(event) => onChange(event.target.value)}
        >
          <option value="">{words.unassigned}</option>
          {value && !current && <option value={value}>{displayName || words.recordedOwner}</option>}
          {people.map((person) => (
            <option key={person.handle} value={person.subject ?? `@${person.handle}`}>
              {person.name || person.handle} (@{person.handle})
            </option>
          ))}
        </select>
      </Label>
      <Label text={words.findPerson(label)}>
        <input
          type="search"
          className={field}
          value={query}
          maxLength={80}
          disabled={disabled}
          onChange={(event) => setQuery(event.target.value)}
        />
      </Label>
      {failed && <p className="text-aui-xs text-aui-muted">{words.ownerSelectionUnavailable}</p>}
    </div>
  );
}
