import GenericBatchDeleteDialog, {
  type BatchDeleteDialogProps,
  type BatchDeleteItem,
} from "@/components/dialog/BatchDeleteDialog";

import { DOCKER_TOAST_META } from "./dockerToast";

/** Batch delete confirmation for Docker resource lists. */
const BatchDeleteDialog = <T extends BatchDeleteItem>(
  props: Omit<BatchDeleteDialogProps<T>, "toastMeta">,
) => <GenericBatchDeleteDialog {...props} toastMeta={DOCKER_TOAST_META} />;

export default BatchDeleteDialog;
