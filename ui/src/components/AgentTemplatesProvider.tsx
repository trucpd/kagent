"use client";

import React, { createContext, useContext, useState, useEffect, ReactNode, useCallback } from "react";
import { getAgentTemplates } from "@/app/actions/agentTemplates";
import type { AgentTemplateResponse } from "@/types";

interface AgentTemplatesContextType {
  agentTemplates: AgentTemplateResponse[];
  loading: boolean;
  error: string;
  refreshAgentTemplates: () => Promise<void>;
}

const AgentTemplatesContext = createContext<AgentTemplatesContextType | undefined>(undefined);

export function useAgentTemplates() {
  const context = useContext(AgentTemplatesContext);
  if (context === undefined) {
    throw new Error("useAgentTemplates must be used within an AgentTemplatesProvider");
  }
  return context;
}

interface AgentTemplatesProviderProps {
  children: ReactNode;
}

export function AgentTemplatesProvider({ children }: AgentTemplatesProviderProps) {
  const [agentTemplates, setAgentTemplates] = useState<AgentTemplateResponse[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  const fetchAgentTemplates = useCallback(async () => {
    try {
      setLoading(true);
      const agentTemplatesResult = await getAgentTemplates();

      if (!agentTemplatesResult.data || agentTemplatesResult.error) {
        throw new Error(agentTemplatesResult.error || "Failed to fetch agent templates");
      }

      setAgentTemplates(agentTemplatesResult.data);
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "An unexpected error occurred");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchAgentTemplates();
  }, [fetchAgentTemplates]);

  const value = {
    agentTemplates,
    loading,
    error,
    refreshAgentTemplates: fetchAgentTemplates,
  };

  return <AgentTemplatesContext.Provider value={value}>{children}</AgentTemplatesContext.Provider>;
}
