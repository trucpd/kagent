"use client";
import { AgentTemplateGrid } from "@/components/AgentTemplateGrid";
import { ErrorState } from "./ErrorState";
import { LoadingState } from "./LoadingState";
import { useAgentTemplates } from "./AgentTemplatesProvider";

export default function AgentTemplateList() {
  const { agentTemplates, loading, error } = useAgentTemplates();

  if (error) {
    return <ErrorState message={error} />;
  }

  if (loading) {
    return <LoadingState />;
  }

  return (
    <div className="mt-12 mx-auto max-w-6xl px-6">
      <div className="flex justify-between items-center mb-8">
        <div className="flex items-center gap-4">
          <h1 className="text-2xl font-bold">Agent Marketplace</h1>
        </div>
      </div>

      {agentTemplates?.length === 0 ? (
        <div className="text-center py-12">
          <h3 className="text-lg font-medium  mb-2">No agent templates found</h3>
          <p className=" mb-6">Create your first agent template to get started</p>
        </div>
      ) : (
        <AgentTemplateGrid agentTemplateResponse={agentTemplates || []} />
      )}
    </div>
  );
}
