import AppTypography from "@/components/ui/AppTypography";

const EmptyState = () => {
  return (
    <AppTypography
      align="center"
      component="div"
      fontWeight={400}
      style={{ padding: 32 }}
      variant="h6"
    >
      This folder is empty.
    </AppTypography>
  );
};

export default EmptyState;
