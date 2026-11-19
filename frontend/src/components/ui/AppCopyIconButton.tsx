import AppActionIconButton from "@/components/ui/AppActionIconButton";

import { AppCopyResultIcon, AppCopyResultStatus } from "./AppCopyResult";
import { COPY_RESULT_LABEL, useCopyResult } from "./useCopyResult";

export interface AppCopyIconButtonProps {
  disabled?: boolean;
  /** Read on the click, so a long text is only built when it is wanted. */
  getText: () => string;
  iconSize?: number;
  label: string;
}

/**
 * An icon button that copies text and says what became of it: for two seconds
 * its icon, colour and tooltip read "Copied" or "Copy failed".
 */
export default function AppCopyIconButton({
  disabled,
  getText,
  iconSize = 18,
  label,
}: AppCopyIconButtonProps) {
  const { copy, result } = useCopyResult();
  return (
    <>
      <AppActionIconButton
        ariaLabel={label}
        className={result ? `app-copy-icon-button--${result}` : undefined}
        disabled={disabled}
        icon={<AppCopyResultIcon result={result} size={iconSize} />}
        label={result ? COPY_RESULT_LABEL[result] : label}
        onClick={() => copy(getText())}
      />
      <AppCopyResultStatus result={result} />
    </>
  );
}
