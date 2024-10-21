"use client";

import React from "react";
import { Typography } from "@mui/material";


const ServiceDetails = ({ params }) => {
  const name = params.Id;
  return (
    <Typography variant="h5" gutterBottom>
      {name}
    </Typography>
  );
};

export default ServiceDetails;
