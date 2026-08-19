declare module 'n8n-workflow' {
  export type IDataObject = Record<string, any>;
  export type INodeProperties = Record<string, any>;
  export type INodeTypeDescription = Record<string, any>;
  export interface INodeExecutionData { json: IDataObject; pairedItem?: any; error?: any }
  export interface INodeType { description: INodeTypeDescription; execute?: any; webhookMethods?: any; webhook?: any }
  export interface IExecuteFunctions {
    getInputData(): INodeExecutionData[];
    getNodeParameter(name: string, index: number, fallback?: any): any;
    getCredentials(name: string): Promise<Record<string, any>>;
    getNode(): any;
    continueOnFail(): boolean;
    helpers: { httpRequestWithAuthentication(name: string, options: Record<string, any>): Promise<any> };
  }
  export interface IHookFunctions {
    getNodeParameter(name: string, fallback?: any): any;
    getCredentials(name: string): Promise<Record<string, any>>;
    getWorkflowStaticData(scope: 'node'|'global'): Record<string, any>;
    getNodeWebhookUrl(name: string): string | undefined;
    getNode(): any;
    getWorkflow(): any;
    helpers: { httpRequestWithAuthentication(name: string, options: Record<string, any>): Promise<any> };
  }
  export interface IWebhookFunctions {
    getNodeParameter(name: string, fallback?: any): any;
    getWorkflowStaticData(scope: 'node'|'global'): Record<string, any>;
    getHeaderData(): Record<string, any>;
    getRequestObject(): any;
    getBodyData(): Record<string, any>;
    getResponseObject(): any;
    getNode(): any;
  }
  export interface IWebhookResponseData { workflowData?: INodeExecutionData[][]; noWebhookResponse?: boolean }
  export interface ICredentialType { [key: string]: any }
  export type IAuthenticateGeneric = Record<string, any>;
  export type ICredentialTestRequest = Record<string, any>;
}
