import type * as acp from "@agentclientprotocol/sdk";

import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";

export default function PermissionCard({
  request,
  onAnswer,
}: {
  request: acp.RequestPermissionRequest;
  onAnswer: (optionId: string | null) => void;
}) {
  return (
    <AppAlert
      severity="warning"
      role="alertdialog"
      aria-label="Permission request"
    >
      <div>
        The agent wants to:{" "}
        {request.toolCall.title ?? request.toolCall.toolCallId}
      </div>
      <div className="assistant__composer">
        {request.options.map((option) => (
          <AppButton
            key={option.optionId}
            color={option.kind.startsWith("reject") ? "error" : "primary"}
            onClick={() => onAnswer(option.optionId)}
            variant={option.kind === "allow_once" ? "contained" : "outlined"}
          >
            {option.name}
          </AppButton>
        ))}
        <AppButton onClick={() => onAnswer(null)}>Cancel turn</AppButton>
      </div>
    </AppAlert>
  );
}
