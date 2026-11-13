/**
 * Real Claude Code plugin-pane messages, recorded by the pane spike against
 * the lot-map plugin. The pushed system messages are verbatim; the answers
 * are the `response` of Claude Code's control_response to each request.
 */

export const paneSessionId = "5feb127c-6ddb-4c9f-a838-3dd13a2562c1";

/** The `capabilities` of the initialize answer. */
export const paneCapabilities: string[] = [
  "ui_surface_v1",
  "prompts_screened_v1",
  "mcp_call_result_not_for_model_v1",
  "mcp_call_check_permissions_v1",
];

/** The answer to ui_attach. */
export const paneAttachAnswer = {
  surfaces: ["vscode"],
};

/** The answer to ui_panes. */
export const panePanesAnswer = {
  panes: [
    {
      id: "lot-map",
      title: "Work map",
      plugin: "lot-map",
    },
  ],
  shown_id: "lot-map",
  focused_id: null,
  focus_requested_id: null,
};

/** The answer to ui_render for the lot-map pane: a Box holding an Svg and Text lines. */
export const paneRenderAnswer = {
  tree: {
    type: "Box",
    props: {
      flexDirection: "column",
    },
    children: [
      {
        type: "Svg",
        props: {
          source:
            '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 460 430" width="920" height="860"><style>@media (prefers-color-scheme: light){[fill="#0a0c11"]{fill:#f5f2ea}[stroke="#0a0c11"]{stroke:#f5f2ea}[fill="#12151c"]{fill:#ffffff}[stroke="#12151c"]{stroke:#ffffff}[fill="#272c36"]{fill:#cfc9bc}[stroke="#272c36"]{stroke:#cfc9bc}[fill="#1a1e27"]{fill:#e3ded2}[stroke="#1a1e27"]{stroke:#e3ded2}[fill="#ebe6dc"]{fill:#1f2228}[stroke="#ebe6dc"]{stroke:#1f2228}[fill="#9ba1ab"]{fill:#5f6570}[stroke="#9ba1ab"]{stroke:#5f6570}[fill="#6b7280"]{fill:#8b909a}[stroke="#6b7280"]{stroke:#8b909a}[fill="#e1ac6e"]{fill:#b8762a}[stroke="#e1ac6e"]{stroke:#b8762a}[fill="#f0c595"]{fill:#8f560f}[stroke="#f0c595"]{stroke:#8f560f}[fill="#e8654f"]{fill:#c5402b}[stroke="#e8654f"]{stroke:#c5402b}[fill="#62c7ba"]{fill:#1c8c81}[stroke="#62c7ba"]{stroke:#1c8c81}[fill="#a5e6dc"]{fill:#136a62}[stroke="#a5e6dc"]{stroke:#136a62}[fill="#ebe6dd"]{fill:#9c978b}[stroke="#ebe6dd"]{stroke:#9c978b}}</style><rect width="460" height="430" rx="12" fill="#0a0c11"/><ellipse cx="230" cy="212" rx="66" ry="62" fill="none" stroke="#272c36" stroke-width=".7" opacity=".45" stroke-dasharray="2 5"/><ellipse cx="230" cy="212" rx="104" ry="97" fill="none" stroke="#272c36" stroke-width=".7" opacity=".45" stroke-dasharray="2 5"/><ellipse cx="230" cy="212" rx="142" ry="132" fill="none" stroke="#272c36" stroke-width=".7" opacity=".45" stroke-dasharray="2 5"/><line x1="230" y1="164" x2="230" y2="70" stroke-width="1.6" stroke-linecap="round" stroke="#1a1e27"/><line x1="230" y1="164" x2="230" y2="70" stroke-width="1.6" stroke-linecap="round" stroke="#e1ac6e" opacity="0.1"/><line x1="259.5" y1="174.1" x2="319.9" y2="96.7" stroke-width="1.4" stroke-linecap="round" stroke="#1a1e27"/><line x1="259.5" y1="174.1" x2="319.9" y2="96.7" stroke-width="1.4" stroke-linecap="round" stroke="#e1ac6e" opacity="0.1"/><line x1="275.9" y1="198.1" x2="376.3" y2="167.7" stroke-width="1" stroke-linecap="round" stroke="#1a1e27"/><line x1="275.9" y1="225.9" x2="376.3" y2="256.3" stroke-width="1" stroke-linecap="round" stroke="#1a1e27"/><line x1="259.5" y1="249.9" x2="319.9" y2="327.3" stroke-width="1" stroke-linecap="round" stroke="#1a1e27"/><line x1="230" y1="260" x2="230" y2="354" stroke-width="1" stroke-linecap="round" stroke="#1a1e27"/><line x1="200.5" y1="249.9" x2="140.1" y2="327.3" stroke-width="1" stroke-linecap="round" stroke="#1a1e27"/><line x1="184.1" y1="225.9" x2="83.7" y2="256.3" stroke-width="1" stroke-linecap="round" stroke="#1a1e27"/><line x1="184.1" y1="198.1" x2="83.7" y2="167.7" stroke-width="1" stroke-linecap="round" stroke="#1a1e27"/><line x1="200.5" y1="174.1" x2="140.1" y2="96.7" stroke-width="1" stroke-linecap="round" stroke="#1a1e27"/><circle cx="230" cy="48" r="32" fill="#e1ac6e" opacity="0"><animate attributeName="opacity" values=".3;0" begin="-2.004s" dur="3.200s" fill="freeze"/></circle><circle cx="333.5" cy="79.3" r="32" fill="#e1ac6e" opacity="0"><animate attributeName="opacity" values=".3;0" begin="-2.012s" dur="3.200s" fill="freeze"/></circle><circle cx="230" cy="48" r="22" fill="#12151c" stroke="#272c36" stroke-width="1.2"/><circle cx="230" cy="48" r="22" fill="none" stroke="#e1ac6e" stroke-width="1.2" opacity="0"><animate attributeName="opacity" values="1;0" begin="-2.004s" dur="3.200s" fill="freeze"/></circle><path transform="translate(230 48)" d="M-3.4 -3.6a3.4 3.4 0 1 0 6.8 0a3.4 3.4 0 1 0 -6.8 0M-6.5 8c0-4 3-6.2 6.5-6.2s6.5 2.2 6.5 6.2" fill="none" stroke="#f0c595" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round"/><text x="230" y="85" text-anchor="middle" font-family="SF Mono, ui-monospace, Menlo, monospace" font-size="11.5" paint-order="stroke" stroke="#0a0c11" stroke-width="3.5" stroke-linejoin="round" fill="#ebe6dc">You</text><text x="230" y="98" text-anchor="middle" font-family="SF Mono, ui-monospace, Menlo, monospace" font-size="9.5" paint-order="stroke" stroke="#0a0c11" stroke-width="3.5" stroke-linejoin="round" fill="#6b7280">2</text><circle cx="333.5" cy="79.3" r="22" fill="#12151c" stroke="#272c36" stroke-width="1.2"/><circle cx="333.5" cy="79.3" r="22" fill="none" stroke="#e1ac6e" stroke-width="1.2" opacity="0"><animate attributeName="opacity" values="1;0" begin="-2.012s" dur="3.200s" fill="freeze"/></circle><path transform="translate(333.5 79.3)" d="M-5 -5h10v10h-10zM-2 -8.5v3.5M2 -8.5v3.5M-2 5v3.5M2 5v3.5M-8.5 -2h3.5M-8.5 2h3.5M5 -2h3.5M5 2h3.5" fill="none" stroke="#f0c595" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round"/><text x="333.5" y="116.3" text-anchor="middle" font-family="SF Mono, ui-monospace, Menlo, monospace" font-size="11.5" paint-order="stroke" stroke="#0a0c11" stroke-width="3.5" stroke-linejoin="round" fill="#ebe6dc">Model</text><text x="333.5" y="129.3" text-anchor="middle" font-family="SF Mono, ui-monospace, Menlo, monospace" font-size="9.5" paint-order="stroke" stroke="#0a0c11" stroke-width="3.5" stroke-linejoin="round" fill="#6b7280">1</text><circle cx="397.4" cy="161.3" r="22" fill="#12151c" stroke="#272c36" stroke-width="1.2"/><path transform="translate(397.4 161.3)" d="M-5 4.5L0 -5L6 3.5M-5 4.5L6 3.5M-7.1 4.5a2.1 2.1 0 1 0 4.2 0a2.1 2.1 0 1 0 -4.2 0M-2.1 -5a2.1 2.1 0 1 0 4.2 0a2.1 2.1 0 1 0 -4.2 0M3.9 3.5a2.1 2.1 0 1 0 4.2 0a2.1 2.1 0 1 0 -4.2 0" fill="none" stroke="#9ba1ab" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round"/><text x="397.4" y="198.3" text-anchor="middle" font-family="SF Mono, ui-monospace, Menlo, monospace" font-size="11.5" paint-order="stroke" stroke="#0a0c11" stroke-width="3.5" stroke-linejoin="round" fill="#9ba1ab">Memory</text><circle cx="397.4" cy="262.7" r="22" fill="#12151c" stroke="#272c36" stroke-width="1.2"/><path transform="translate(397.4 262.7)" d="M-5.5 -8h7.5l3.5 3.5v12.5h-11zM2 -8v3.5h3.5" fill="none" stroke="#9ba1ab" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round"/><text x="397.4" y="299.7" text-anchor="middle" font-family="SF Mono, ui-monospace, Menlo, monospace" font-size="11.5" paint-order="stroke" stroke="#0a0c11" stroke-width="3.5" stroke-linejoin="round" fill="#9ba1ab">Files</text><circle cx="333.5" cy="344.7" r="22" fill="#12151c" stroke="#272c36" stroke-width="1.2"/><path transform="translate(333.5 344.7)" d="M-7.5 -5l5 5l-5 5M0 6h7.5" fill="none" stroke="#9ba1ab" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round"/><text x="333.5" y="381.7" text-anchor="middle" font-family="SF Mono, ui-monospace, Menlo, monospace" font-size="11.5" paint-order="stroke" stroke="#0a0c11" stroke-width="3.5" stroke-linejoin="round" fill="#9ba1ab">Shell</text><circle cx="230" cy="376" r="22" fill="#12151c" stroke="#272c36" stroke-width="1.2"/><path transform="translate(230 376)" d="M-7.8 0a7.8 7.8 0 1 0 15.6 0a7.8 7.8 0 1 0 -15.6 0M-7.8 0h15.6M0 -7.8c-4.2 4.2 -4.2 11.4 0 15.6M0 -7.8c4.2 4.2 4.2 11.4 0 15.6" fill="none" stroke="#9ba1ab" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round"/><text x="230" y="413" text-anchor="middle" font-family="SF Mono, ui-monospace, Menlo, monospace" font-size="11.5" paint-order="stroke" stroke="#0a0c11" stroke-width="3.5" stroke-linejoin="round" fill="#9ba1ab">Network</text><circle cx="126.5" cy="344.7" r="22" fill="#12151c" stroke="#272c36" stroke-width="1.2"/><path transform="translate(126.5 344.7)" d="M-8.5 -6.5h17v13h-17zM-8.5 -2.8h17M-6.3 -4.7h.1M-3.8 -4.7h.1" fill="none" stroke="#9ba1ab" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round"/><text x="126.5" y="381.7" text-anchor="middle" font-family="SF Mono, ui-monospace, Menlo, monospace" font-size="11.5" paint-order="stroke" stroke="#0a0c11" stroke-width="3.5" stroke-linejoin="round" fill="#9ba1ab">Browser</text><circle cx="62.6" cy="262.7" r="22" fill="#12151c" stroke="#272c36" stroke-width="1.2"/><path transform="translate(62.6 262.7)" d="M0 -4v3.5M0 -.5L-6 4M0 -.5L6 4M-2.1 -6a2.1 2.1 0 1 0 4.2 0a2.1 2.1 0 1 0 -4.2 0M-8.6 5.5a2.1 2.1 0 1 0 4.2 0a2.1 2.1 0 1 0 -4.2 0M4.4 5.5a2.1 2.1 0 1 0 4.2 0a2.1 2.1 0 1 0 -4.2 0" fill="none" stroke="#9ba1ab" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round"/><text x="62.6" y="299.7" text-anchor="middle" font-family="SF Mono, ui-monospace, Menlo, monospace" font-size="11.5" paint-order="stroke" stroke="#0a0c11" stroke-width="3.5" stroke-linejoin="round" fill="#9ba1ab">Agents</text><circle cx="62.6" cy="161.3" r="22" fill="#12151c" stroke="#272c36" stroke-width="1.2"/><path transform="translate(62.6 161.3)" d="M1.5 -8.5L-5.5 1h4.5l-1.5 7.5l7.5 -9.8h-4.5z" fill="none" stroke="#9ba1ab" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round"/><text x="62.6" y="198.3" text-anchor="middle" font-family="SF Mono, ui-monospace, Menlo, monospace" font-size="11.5" paint-order="stroke" stroke="#0a0c11" stroke-width="3.5" stroke-linejoin="round" fill="#9ba1ab">Skills</text><circle cx="126.5" cy="79.3" r="22" fill="#12151c" stroke="#272c36" stroke-width="1.2"/><path transform="translate(126.5 79.3)" d="M2.5 -5.5v8a4.2 4.2 0 1 1 -8.4 0v-1.5M0.7 -7.3a1.8 1.8 0 1 0 3.6 0a1.8 1.8 0 1 0 -3.6 0" fill="none" stroke="#9ba1ab" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round"/><text x="126.5" y="116.3" text-anchor="middle" font-family="SF Mono, ui-monospace, Menlo, monospace" font-size="11.5" paint-order="stroke" stroke="#0a0c11" stroke-width="3.5" stroke-linejoin="round" fill="#9ba1ab">Hooks</text><circle cx="230" cy="212" r="46" fill="#e1ac6e" opacity=".08"><animate attributeName="r" values="43;53;43" begin="-2.268s" dur="4.800s" repeatCount="indefinite"/><animate attributeName="opacity" values=".05;.12;.05" begin="-2.268s" dur="4.800s" repeatCount="indefinite"/></circle><circle cx="230" cy="212" r="40" fill="#12151c" stroke="#272c36" stroke-width="1.3"/><circle cx="230" cy="212" r="45" fill="none" stroke-width="2.2" stroke="#272c36" opacity=".7"/><circle cx="230" cy="212" r="45" fill="none" stroke-width="2.2" stroke="#9ba1ab" pathLength="100" stroke-dasharray="3 100" transform="rotate(-90 230 212)"/><text x="230" y="193" text-anchor="middle" font-family="SF Mono, ui-monospace, Menlo, monospace" font-size="8" fill="#6b7280">3%</text><text x="230" y="213" text-anchor="middle" font-family="SF Mono, ui-monospace, Menlo, monospace" font-size="13.8" letter-spacing="2" fill="#ebe6dc">CLAUDE</text><text x="230" y="229" text-anchor="middle" font-family="SF Mono, ui-monospace, Menlo, monospace" font-size="9" fill="#9ba1ab">fable 5.2</text></svg>',
          alt: "Work map. Nothing is running. · context 3%",
        },
      },
      {
        type: "Text",
        props: {
          mono: true,
          size: "footnote",
        },
        children: ["Nothing is running. · context 3%"],
      },
      {
        type: "Text",
        props: {
          mono: true,
          size: "footnote",
          dimColor: true,
        },
        children: ["· You      answer ready"],
      },
      {
        type: "Text",
        props: {
          mono: true,
          size: "footnote",
          dimColor: true,
        },
        children: ["· Model    fable 5.2 · step 1  3.2 s"],
      },
      {
        type: "Text",
        props: {
          mono: true,
          size: "footnote",
          dimColor: true,
        },
        children: ["· You      new message"],
      },
    ],
    part: {
      plugin: "lot-map",
    },
  },
  props: {},
  rewritten: false,
  hooked: true,
  drawn_by: ["lot-map"],
};

/** Pushed on stdout as Claude Code runs. */
export const panePushedLog = {
  type: "system",
  subtype: "ui_log",
  plugin: "lot-map",
  text: 'its classic.SessionStart hook was skipped. "cc-plugin-sec-default" runs first and sent the event on past it.',
  uuid: "3aeda32e-7258-4cc6-8889-9306c600052d",
  session_id: "5feb127c-6ddb-4c9f-a838-3dd13a2562c1",
};

export const panePushedRoster = {
  type: "system",
  subtype: "ui_panes",
  panes: [
    {
      id: "lot-map",
      title: "Work map",
      plugin: "lot-map",
    },
  ],
  shown_id: "lot-map",
  focused_id: null,
  focus_requested_id: null,
  uuid: "d88c4e18-822a-4183-b521-677f6bfdf189",
  session_id: "5feb127c-6ddb-4c9f-a838-3dd13a2562c1",
};

export const panePushedInvalidate = {
  type: "system",
  subtype: "ui_invalidate",
  event: "ui.render",
  prompts_screened: true,
  instances: [
    {
      surface: "vscode",
      component: "Pane",
      instance_id: "lot-map",
    },
  ],
  uuid: "d10ceb2d-73ce-4bbd-b0d7-d35ed2b30e99",
  session_id: "5feb127c-6ddb-4c9f-a838-3dd13a2562c1",
};
