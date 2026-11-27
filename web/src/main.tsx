import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { makeQueryClient } from "@/api/queryClient";
import { TooltipProvider } from "@/components/ui/tooltip";
import { followSystemTheme } from "@/lib/theme";
import { App } from "./App";
import "./index.css";

followSystemTheme();

const queryClient = makeQueryClient();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <App />
      </TooltipProvider>
    </QueryClientProvider>
  </StrictMode>,
);
