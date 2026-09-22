import type { AppDialogProps } from "@/components/ui/AppDialog";
import { AppDialog } from "@/components/ui/AppDialog";

type GeneralDialogProps = AppDialogProps;

const GeneralDialog = (props: GeneralDialogProps) => <AppDialog {...props} />;

export default GeneralDialog;
