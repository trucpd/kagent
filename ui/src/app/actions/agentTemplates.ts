"use server";

import { AgentTemplate, AgentTemplateResponse, BaseResponse } from "@/types";
import { revalidatePath } from "next/cache";
import { fetchApi, createErrorResponse } from "./utils";
import { k8sRefUtils } from "@/lib/k8sUtils";

/**
 * Gets all agent templates
 * @returns A promise with all agent templates
 */
export async function getAgentTemplates(): Promise<BaseResponse<AgentTemplateResponse[]>> {
  try {
    const { data } = await fetchApi<BaseResponse<AgentTemplateResponse[]>>(`/agent-templates`);

    const sortedData = data?.sort((a, b) => {
      const aRef = k8sRefUtils.toRef(a.agentTemplate.metadata.namespace || "", a.agentTemplate.metadata.name);
      const bRef = k8sRefUtils.toRef(b.agentTemplate.metadata.namespace || "", b.agentTemplate.metadata.name);
      return aRef.localeCompare(bRef);
    });

    return { message: "Successfully fetched agent templates", data: sortedData };
  } catch (error) {
    return createErrorResponse<AgentTemplateResponse[]>(error, "Error getting agent templates");
  }
}
