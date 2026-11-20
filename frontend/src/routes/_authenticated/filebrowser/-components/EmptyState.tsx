import AppTypography from "@/components/ui/AppTypography";

const EmptyState = ({ message = "This folder is empty." }) => {
  return (
    <AppTypography
      align="center"
      component="div"
      fontWeight={400}
      style={{ padding: 32 }}
      variant="h6"
    >
      {message}
    </AppTypography>
  );
};

export default EmptyState;
