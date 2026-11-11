import { BorderBeam } from "border-beam";
import { VoiceBeam } from "voice-glow";

/** The light is laid over the bar, to its border's edge and corners, and never in the way of a click. */
const over = { position: "absolute", inset: -1, pointerEvents: "none" } as const;
const radiusOf = (host: HTMLElement | null) =>
  (host ? Number.parseFloat(getComputedStyle(host).borderTopLeftRadius) : 0) || 16;
const theme = () => (document.documentElement.dataset.theme === "dark" ? "dark" : "light");

/**
 * The bar's border while a reply is in flight: a beam of the brand's warm
 * colors travels round it, so the system reads as working. It fades in and
 * out with `working`, and holds still under reduced motion (border-beam).
 */
export function WorkingBeam({ working, host }: { working: boolean; host: HTMLElement | null }) {
  return (
    <BorderBeam
      aria-hidden
      data-working-beam
      size="md"
      colorVariant="sunset"
      active={working}
      theme={theme()}
      borderRadius={radiusOf(host)}
      style={over}
    >
      <span className="block h-full" />
    </BorderBeam>
  );
}

/**
 * The microphone's level along the bottom edge of the bar while a voice note
 * records: the light rises with the voice, so the person sees they are heard
 * before they stop. In the same colors as the working beam. It is a meter,
 * so it still follows the voice under reduced motion; voice-glow stills only
 * its own idle drift there.
 */
export function VoiceLevel({ stream, host }: { stream: MediaStream; host: HTMLElement | null }) {
  return (
    <VoiceBeam
      aria-hidden
      data-voice-level
      stream={stream}
      colorVariant="sunset"
      theme={theme()}
      borderRadius={radiusOf(host)}
      style={over}
    >
      <span className="block h-full" />
    </VoiceBeam>
  );
}
