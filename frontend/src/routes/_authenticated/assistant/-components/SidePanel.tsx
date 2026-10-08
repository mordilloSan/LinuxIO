import { Icon } from "@iconify/react";
import { useState } from "react";

import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert from "@/components/ui/AppAlert";
import AppPaper from "@/components/ui/AppPaper";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";

import { assistantStore, type SideSession } from "./assistant-store";
import PermissionCard from "./PermissionCard";
import Transcript from "./Transcript";

/** A forked chat beside the main one; its stream never touches the main transcript. */
export default function SidePanel({ side }: { side: SideSession }) {
  const [draft, setDraft] = useState("");
  const running = side.status === "running";

  const send = () => {
    const text = draft.trim();
    if (!text || running) return;
    setDraft("");
    void assistantStore.sendSide(text);
  };

  return (
    <AppPaper
      aria-label="Side question"
      className="assistant__side"
      role="complementary"
      variant="outlined"
    >
      <div className="assistant__side-header">
        <Icon height={18} icon="mdi:comment-question-outline" width={18} />
        <AppTypography className="assistant__side-title" fontWeight={600}>
          Side question
        </AppTypography>
        <AppActionIconButton
          ariaLabel="Discard side question"
          icon="mdi:delete-outline"
          onClick={() => void assistantStore.discardSide()}
          tooltip={false}
        />
        <AppActionIconButton
          ariaLabel="Close side question"
          icon="mdi:close"
          onClick={() => assistantStore.hideSide()}
          tooltip={false}
        />
      </div>
      <Transcript
        blocks={side.blocks}
        label="Side question"
        sessionId={side.sessionId}
      />
      {side.pending ? (
        <PermissionCard
          request={side.pending.request}
          onAnswer={(optionId) => assistantStore.answerSidePermission(optionId)}
        />
      ) : null}
      {side.error ? <AppAlert severity="error">{side.error}</AppAlert> : null}
      <div className="assistant__side-composer">
        <AppTextField
          aria-label="Side question message"
          fullWidth
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.shiftKey) {
              event.preventDefault();
              send();
            }
          }}
          size="small"
          value={draft}
        />
        {running ? (
          <AppActionIconButton
            ariaLabel="Stop side question"
            color="var(--app-palette-error-main)"
            icon="mdi:stop"
            onClick={() => assistantStore.stopSide()}
            tooltip={false}
          />
        ) : (
          <AppActionIconButton
            ariaLabel="Send side question"
            disabled={!draft.trim()}
            icon="mdi:send"
            onClick={send}
            tooltip={false}
          />
        )}
      </div>
    </AppPaper>
  );
}
