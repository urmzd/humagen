/**
 * The mascots' tools, drawn as small rendered objects: rounded, bevelled
 * forms in glossy plastic, chrome, rubber, glass and wood, lit by one soft
 * key light from the top left with a rim light behind, specular highlights
 * and soft contact shadows. Only gradients, no filters, so they move freely.
 * `u` resolves a material by name to this mascot's own gradient.
 */
type U = (name: string) => string;

/** The materials, one set per mascot, named with its id. */
export function PropDefs({ id }: { id: string }) {
  const n = (s: string) => `${id}-${s}`;
  return (
    <>
      <linearGradient id={n("plastic")} x1="0" y1="0" x2="1" y2="1">
        <stop offset="0" stopColor="#ffffff" />
        <stop offset="0.45" stopColor="#eef2f7" />
        <stop offset="1" stopColor="#b4bfcc" />
      </linearGradient>
      <linearGradient id={n("plasticV")} x1="0" y1="0" x2="1" y2="0">
        <stop offset="0" stopColor="#c4ced9" />
        <stop offset="0.3" stopColor="#ffffff" />
        <stop offset="0.7" stopColor="#e7ecf2" />
        <stop offset="1" stopColor="#a9b5c3" />
      </linearGradient>
      <linearGradient id={n("rubber")} x1="0" y1="0" x2="0" y2="1">
        <stop offset="0" stopColor="#4b525d" />
        <stop offset="0.25" stopColor="#2a2f37" />
        <stop offset="1" stopColor="#101216" />
      </linearGradient>
      <linearGradient id={n("chrome")} x1="0" y1="0" x2="0" y2="1">
        <stop offset="0" stopColor="#fbfcfe" />
        <stop offset="0.32" stopColor="#a5aeb9" />
        <stop offset="0.5" stopColor="#eef2f6" />
        <stop offset="0.78" stopColor="#66707c" />
        <stop offset="1" stopColor="#c9d1d9" />
      </linearGradient>
      <linearGradient id={n("chromeH")} x1="0" y1="0" x2="1" y2="0">
        <stop offset="0" stopColor="#7b8591" />
        <stop offset="0.3" stopColor="#f4f6f9" />
        <stop offset="0.55" stopColor="#a9b2bd" />
        <stop offset="0.8" stopColor="#eef1f4" />
        <stop offset="1" stopColor="#737d89" />
      </linearGradient>
      <radialGradient id={n("knob")} cx="0.35" cy="0.3" r="0.8">
        <stop offset="0" stopColor="#ffffff" />
        <stop offset="0.45" stopColor="#c7ced6" />
        <stop offset="1" stopColor="#59636e" />
      </radialGradient>
      <linearGradient id={n("gold")} x1="0" y1="0" x2="0" y2="1">
        <stop offset="0" stopColor="#ffe39a" />
        <stop offset="0.5" stopColor="#e0a93c" />
        <stop offset="1" stopColor="#9c6a1c" />
      </linearGradient>
      <linearGradient id={n("glass")} x1="0" y1="0" x2="1" y2="1">
        <stop offset="0" stopColor="#f2fbff" stopOpacity="0.95" />
        <stop offset="1" stopColor="#8cc6ec" stopOpacity="0.85" />
      </linearGradient>
      <radialGradient id={n("glow")} cx="0.5" cy="0.5" r="0.5">
        <stop offset="0" stopColor="#fffbe6" />
        <stop offset="0.4" stopColor="#ffe08a" stopOpacity="0.8" />
        <stop offset="1" stopColor="#ffc83d" stopOpacity="0" />
      </radialGradient>
      <linearGradient id={n("beam")} x1="0" y1="1" x2="0" y2="0">
        <stop offset="0" stopColor="#fff2b3" stopOpacity="0.85" />
        <stop offset="1" stopColor="#fff2b3" stopOpacity="0" />
      </linearGradient>
      <radialGradient id={n("contact")} cx="0.5" cy="0.5" r="0.5">
        <stop offset="0" stopColor="#000" stopOpacity="0.42" />
        <stop offset="1" stopColor="#000" stopOpacity="0" />
      </radialGradient>
      <radialGradient id={n("puff")} cx="0.36" cy="0.28" r="0.85">
        <stop offset="0" stopColor="#ffffff" />
        <stop offset="0.55" stopColor="#f0f4f9" />
        <stop offset="1" stopColor="#bccadb" />
      </radialGradient>
      <radialGradient id={n("bead")} cx="0.35" cy="0.3" r="0.75">
        <stop offset="0" stopColor="#b8c2cf" />
        <stop offset="0.5" stopColor="#5d6876" />
        <stop offset="1" stopColor="#2a313b" />
      </radialGradient>
      <radialGradient id={n("blue")} cx="0.35" cy="0.25" r="0.9">
        <stop offset="0" stopColor="#a9d0ff" />
        <stop offset="0.5" stopColor="#4b8ff2" />
        <stop offset="1" stopColor="#1f57c4" />
      </radialGradient>
      <radialGradient id={n("pearl")} cx="0.35" cy="0.3" r="0.8">
        <stop offset="0" stopColor="#ffffff" />
        <stop offset="1" stopColor="#d8e3f0" />
      </radialGradient>
      <linearGradient id={n("yellowA")} x1="0" y1="0" x2="1" y2="0">
        <stop offset="0" stopColor="#f6b51c" />
        <stop offset="1" stopColor="#ffcf3f" />
      </linearGradient>
      <linearGradient id={n("yellowB")} x1="0" y1="0" x2="1" y2="0">
        <stop offset="0" stopColor="#ffe27a" />
        <stop offset="1" stopColor="#ffd34d" />
      </linearGradient>
      <linearGradient id={n("yellowC")} x1="0" y1="0" x2="1" y2="0">
        <stop offset="0" stopColor="#e9a512" />
        <stop offset="1" stopColor="#c7850a" />
      </linearGradient>
      <linearGradient id={n("eraser")} x1="0" y1="0" x2="1" y2="0">
        <stop offset="0" stopColor="#ffb3c1" />
        <stop offset="0.5" stopColor="#ff8ea4" />
        <stop offset="1" stopColor="#d9607a" />
      </linearGradient>
      <linearGradient id={n("wood")} x1="0" y1="0" x2="1" y2="0">
        <stop offset="0" stopColor="#f7deb6" />
        <stop offset="1" stopColor="#d6a467" />
      </linearGradient>
      <linearGradient id={n("oak")} x1="0" y1="0" x2="0" y2="1">
        <stop offset="0" stopColor="#d8965a" />
        <stop offset="0.5" stopColor="#a8642e" />
        <stop offset="1" stopColor="#6b3a17" />
      </linearGradient>
      <linearGradient id={n("sand")} x1="0" y1="0" x2="1" y2="1">
        <stop offset="0" stopColor="#ffd98a" />
        <stop offset="1" stopColor="#d9962f" />
      </linearGradient>
      <linearGradient id={n("paper")} x1="0" y1="0" x2="1" y2="1">
        <stop offset="0" stopColor="#ffffff" />
        <stop offset="1" stopColor="#e6e9ee" />
      </linearGradient>
      <linearGradient id={n("gloss")} x1="0" y1="0" x2="1" y2="1">
        <stop offset="0" stopColor="#4a505b" />
        <stop offset="0.5" stopColor="#1e2228" />
        <stop offset="1" stopColor="#0b0c0f" />
      </linearGradient>
      <radialGradient id={n("led")} cx="0.5" cy="0.5" r="0.5">
        <stop offset="0" stopColor="#ffe1e1" />
        <stop offset="0.35" stopColor="#ff4a4a" />
        <stop offset="1" stopColor="#ff2a2a" stopOpacity="0" />
      </radialGradient>
    </>
  );
}

/** A soft contact shadow where a thing meets the ground. */
function Contact({ u, cx, cy, rx }: { u: U; cx: number; cy: number; rx: number }) {
  return <ellipse cx={cx} cy={cy} rx={rx} ry={rx * 0.16} fill={u("contact")} />;
}

/**
 * A lab microscope standing at the bot's right, its arm toward the bot: the
 * bot turns the coarse focus knob on the arm, the tube eases down, the lamp
 * shines up through the slide, and a glint crosses the lens.
 */
export function Microscope({ u }: { u: U }) {
  return (
    <g>
      <Contact u={u} cx={103} cy={106.5} rx={22} />
      {/* Base: a bevelled rubber foot. */}
      <rect x="84" y="99" width="37" height="7" rx="3.2" fill={u("rubber")} />
      <rect x="86" y="99.6" width="33" height="1.2" rx="0.6" fill="#fff" opacity="0.22" />
      {/* The light shines up through the stage. */}
      <path className="p-beam" d="M104 92L101 84H109L106 92Z" fill={u("beam")} />
      <rect x="102" y="91" width="6" height="5" rx="1.6" fill={u("chromeH")} />
      <ellipse className="p-lamp" cx="105" cy="91" rx="5" ry="2.6" fill={u("glow")} />
      {/* Arm: a thick C of white plastic from the base up to the tube. */}
      <path
        d="M91 101C80 90 83 70 99 63"
        stroke={u("plasticV")}
        strokeWidth="8"
        strokeLinecap="round"
        fill="none"
      />
      <path
        d="M89.6 98C81.5 88 84 72 97 65.6"
        stroke="#fff"
        strokeOpacity="0.7"
        strokeWidth="1.3"
        strokeLinecap="round"
        fill="none"
      />
      {/* Stage with a glass slide; a glint crosses it. */}
      <rect x="91" y="84" width="27" height="4.4" rx="1.8" fill={u("rubber")} />
      <rect x="92" y="84.4" width="25" height="0.9" rx="0.45" fill="#fff" opacity="0.25" />
      <rect x="99" y="82.3" width="13" height="2" rx="0.6" fill={u("glass")} />
      <rect className="p-glint" x="101" y="82.5" width="2.2" height="1.5" rx="0.4" fill="#fff" />
      {/* The tube focuses: eyepiece, body, nosepiece, objective with its gold ring. */}
      <g className="p-focus">
        <g transform="rotate(-22 106 80)">
          <rect x="102.6" y="72" width="6.8" height="3.4" rx="1.2" fill={u("chrome")} />
          <rect x="104" y="75" width="4" height="5.4" rx="1" fill={u("chrome")} />
          <rect x="104" y="77" width="4" height="1" fill={u("gold")} />
          <rect x="101.5" y="47" width="9" height="25.6" rx="3.6" fill={u("plasticV")} />
          <rect x="102.6" y="48" width="1.6" height="23" rx="0.8" fill="#fff" opacity="0.8" />
          <rect x="101.5" y="58" width="9" height="2.4" fill="#2b3038" />
          <rect x="102.6" y="40" width="6.8" height="8" rx="1.6" fill={u("gloss")} />
          <rect x="102" y="39" width="8" height="2.4" rx="1.2" fill={u("chrome")} />
          <rect x="103.4" y="41.6" width="1.2" height="5.6" rx="0.6" fill="#fff" opacity="0.35" />
        </g>
      </g>
      {/* The coarse focus knob on the arm, under the bot's hand, and the fine one below. */}
      <circle cx="88" cy="93" r="2.6" fill={u("knob")} />
      <g className="p-knob">
        <circle cx="88" cy="86" r="4.6" fill={u("knob")} stroke="#4d5661" strokeWidth="0.5" />
        <path
          d="M88 81.4v1.6M88 89v1.6M83.4 86h1.6M91 86h1.6M84.8 82.8l1.1 1.1M90.1 88.1l1.1 1.1M84.8 89.2l1.1-1.1M90.1 83.9l1.1-1.1"
          stroke="#4d5661"
          strokeWidth="0.7"
          strokeLinecap="round"
        />
        <circle cx="86.6" cy="84.6" r="1.2" fill="#fff" opacity="0.8" />
      </g>
    </g>
  );
}

/** A puffy cloud of thought that breathes, three beads rising one after another inside it. */
export function ThoughtCloud({ u }: { u: U }) {
  return (
    <g transform="translate(78 -2)">
      <circle className="p-trail" cx="6" cy="44" r="2.4" fill={u("puff")} />
      <circle className="p-trail p-late" cx="12" cy="37" r="3.6" fill={u("puff")} />
      <g className="p-cloud">
        <ellipse cx="31" cy="35" rx="20" ry="4" fill={u("contact")} opacity="0.5" />
        <circle cx="20" cy="24" r="9" fill={u("puff")} />
        <circle cx="31" cy="17" r="11" fill={u("puff")} />
        <circle cx="43" cy="22" r="9.5" fill={u("puff")} />
        <rect x="14" y="22" width="36" height="12" rx="6" fill={u("puff")} />
        <path
          d="M24 12a9 9 0 0 1 10-4"
          stroke="#fff"
          strokeWidth="2"
          strokeLinecap="round"
          fill="none"
        />
        <circle className="p-bead" cx="23" cy="26" r="2.5" fill={u("bead")} />
        <circle className="p-bead" cx="31.5" cy="26" r="2.5" fill={u("bead")} />
        <circle className="p-bead" cx="40" cy="26" r="2.5" fill={u("bead")} />
      </g>
    </g>
  );
}

/** Two inflated chat bubbles that pop up in turn; the answer's beads type. */
export function Bubbles({ u }: { u: U }) {
  return (
    <g transform="translate(82 2)">
      <g className="p-pop">
        <path
          d="M6 2h18a5 5 0 0 1 5 5v7a5 5 0 0 1-5 5H13l-6 4.6V19H6a5 5 0 0 1-5-5V7a5 5 0 0 1 5-5z"
          fill={u("pearl")}
        />
        <path
          d="M5 5.5q2-2.4 6-2.4"
          stroke="#fff"
          strokeWidth="1.6"
          strokeLinecap="round"
          fill="none"
        />
        <path d="M7 9.5h14M7 13.5h9" stroke="#9aa8b8" strokeWidth="1.6" strokeLinecap="round" />
      </g>
      <g className="p-pop p-late">
        <path
          d="M23 20h18a5 5 0 0 1 5 5v7a5 5 0 0 1-5 5h-1v4.6L34 37H23a5 5 0 0 1-5-5v-7a5 5 0 0 1 5-5z"
          fill={u("blue")}
        />
        <path
          d="M22 23.5q2-2.4 6-2.4"
          stroke="#fff"
          strokeOpacity="0.8"
          strokeWidth="1.6"
          strokeLinecap="round"
          fill="none"
        />
        <circle className="p-bead" cx="26" cy="28.6" r="2" fill="#fff" />
        <circle className="p-bead" cx="32" cy="28.6" r="2" fill="#fff" />
        <circle className="p-bead" cx="38" cy="28.6" r="2" fill="#fff" />
      </g>
    </g>
  );
}

/** A wooden hourglass of glass and sand. It holds still: it waits on a person. */
export function Hourglass({ u }: { u: U }) {
  return (
    <g transform="translate(90 70)">
      <Contact u={u} cx={16} cy={36.6} rx={16} />
      <path
        d="M9 6c0 8 6 9.5 6 12s-6 4-6 12h14c0-8-6-9.5-6-12s6-4 6-12z"
        fill={u("glass")}
        opacity="0.55"
      />
      <path d="M10.8 10.5c1 3.4 3.6 5 4.2 7h2c.6-2 3.2-3.6 4.2-7z" fill={u("sand")} />
      <rect x="15.6" y="17.5" width="0.8" height="8" fill={u("sand")} />
      <path d="M10 30c1.2-4 3.8-5.2 6-5.2s4.8 1.2 6 5.2z" fill={u("sand")} />
      <path
        d="M10.6 8c.4 3 2 5 3.4 6.4"
        stroke="#fff"
        strokeOpacity="0.8"
        strokeWidth="1.1"
        strokeLinecap="round"
        fill="none"
      />
      <path
        d="M11 27.6c.6-2 1.6-3 2.6-3.6"
        stroke="#fff"
        strokeOpacity="0.6"
        strokeWidth="1"
        strokeLinecap="round"
        fill="none"
      />
      <rect x="5.4" y="5" width="2.6" height="26" rx="1.2" fill={u("oak")} />
      <rect x="24" y="5" width="2.6" height="26" rx="1.2" fill={u("oak")} />
      <rect x="3" y="1.6" width="26" height="4.4" rx="2" fill={u("oak")} />
      <rect x="3" y="30" width="26" height="4.4" rx="2" fill={u("oak")} />
      <rect x="4.4" y="2.2" width="23" height="1" rx="0.5" fill="#fff" opacity="0.35" />
      <rect x="4.4" y="30.6" width="23" height="1" rx="0.5" fill="#fff" opacity="0.35" />
    </g>
  );
}

/** A spiral notepad held up to write on; its lines fill in as the pencil moves. */
export function Notepad({ u }: { u: U }) {
  return (
    <g transform="translate(-4 -11)">
      <rect x="1" y="2.4" width="26" height="23" rx="2.4" fill="#000" opacity="0.18" />
      <rect width="26" height="23" rx="2.4" fill={u("paper")} />
      <rect width="26" height="4" rx="2" fill="#d6463b" />
      {[4, 9, 14, 19].map((x) => (
        <rect key={x} x={x} y="-1.6" width="2" height="4.4" rx="1" fill={u("chrome")} />
      ))}
      <path
        className="p-ink"
        d="M4 9h17M4 13.4h13M4 17.8h15"
        stroke="#3d4450"
        strokeWidth="1.2"
        strokeLinecap="round"
        pathLength={1}
      />
    </g>
  );
}

/** A faceted yellow pencil with a chrome ferrule, a pink eraser and a sharp tip, held to write. */
export function Pencil({ u }: { u: U }) {
  return (
    <g transform="rotate(25) translate(-16 -22)">
      <rect x="12.6" y="0.8" width="6.8" height="5.6" rx="2.6" fill={u("eraser")} />
      <rect x="12.6" y="5.4" width="6.8" height="4" fill={u("chromeH")} />
      <path d="M12.6 6.6h6.8M12.6 8h6.8" stroke="#6f7984" strokeWidth="0.45" />
      <rect x="12.6" y="9.2" width="2.3" height="14" fill={u("yellowA")} />
      <rect x="14.9" y="9.2" width="2.3" height="14" fill={u("yellowB")} />
      <rect x="17.2" y="9.2" width="2.2" height="14" fill={u("yellowC")} />
      <rect x="15.4" y="9.6" width="0.8" height="13" rx="0.4" fill="#fff" opacity="0.7" />
      <path d="M12.6 23.2h6.8L16 30.4z" fill={u("wood")} />
      <path d="M14.9 27.6h2.2L16 30.4z" fill="#2d2f33" />
      <circle cx="15.7" cy="29" r="0.35" fill="#fff" opacity="0.8" />
    </g>
  );
}

/** A chrome wrench, its bevel catching the light; sparks fly from its jaw as it turns. */
export function Wrench({ u }: { u: U }) {
  const shape =
    "M20.8 4.4a6.8 6.8 0 0 0-7.6 9.4l-8.6 8.6a3 3 0 0 0 4.2 4.2l8.6-8.6a6.8 6.8 0 0 0 9.4-7.6l-4.2 4.2-4.4-1-1-4.4z";
  return (
    <g transform="translate(-6 -22) scale(1.15)">
      <path
        d={shape}
        fill={u("chrome")}
        stroke="#4d5661"
        strokeWidth="0.6"
        strokeLinejoin="round"
      />
      <path
        d="M6 23.6l9.4-9.4"
        stroke="#fff"
        strokeOpacity="0.85"
        strokeWidth="0.9"
        strokeLinecap="round"
      />
      <path
        d="M15.2 6.2a5.4 5.4 0 0 1 4-1.4"
        stroke="#fff"
        strokeOpacity="0.9"
        strokeWidth="0.8"
        strokeLinecap="round"
        fill="none"
      />
      <circle cx="6.7" cy="24.5" r="1.1" fill="#3b424b" />
      <g className="p-sparks" stroke="#ffd25a" strokeWidth="0.9" strokeLinecap="round">
        <path className="p-spark" d="M27 4l3-2" />
        <path className="p-spark p-late" d="M28 8h3.4" />
        <path className="p-spark p-later" d="M25 1l1.2-3" />
      </g>
    </g>
  );
}

/** The agent's remote: glossy black with a chrome edge, its light blinking; it sends its signal. */
export function Remote({ u, signal }: { u: U; signal?: boolean }) {
  return (
    <g transform="translate(-3 -17)">
      <rect
        width="6.4"
        height="17"
        rx="2.2"
        fill={u("gloss")}
        stroke={u("chromeH")}
        strokeWidth="0.7"
      />
      <rect x="1" y="1.2" width="1" height="14" rx="0.5" fill="#fff" opacity="0.25" />
      <rect x="1.6" y="6.4" width="3.2" height="1.4" rx="0.7" fill="#4a515c" />
      <rect x="1.6" y="9.2" width="3.2" height="1.4" rx="0.7" fill="#4a515c" />
      <circle className="p-led" cx="3.2" cy="3" r="3.4" fill={u("led")} />
      <circle cx="3.2" cy="3" r="1.1" fill="#ff5a5a" />
      {signal && (
        <g fill="none" stroke="#ff8a8a" strokeWidth="1" strokeLinecap="round">
          <path className="p-signal" d="M-0.6 -2a5.4 5.4 0 0 1 7.6 0" />
          <path className="p-signal p-late" d="M-3.4 -4.8a9.4 9.4 0 0 1 13.2 0" />
        </g>
      )}
    </g>
  );
}
