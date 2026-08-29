declare module "react" {
  export type ReactNode = unknown;
  export interface CSSProperties { [property: string]: string | number | undefined; }
  export type SetStateAction<T> = T | ((previous: T) => T);
  export type Dispatch<T> = (value: T) => void;
  export interface RefObject<T> { current: T; }
  export const StrictMode: (props: {children?: ReactNode}) => unknown;
  export const Suspense: (props: {fallback?: ReactNode; children?: ReactNode}) => unknown;
  export function lazy<T extends (props: any) => unknown>(loader: () => Promise<{default: T}>): T;
  export interface Context<T> { Provider: (props: {value: T; children?: ReactNode}) => unknown; }
  export function createContext<T>(initial: T): Context<T>;
  export function useContext<T>(context: Context<T>): T;
  export function useCallback<T extends (...args: any[]) => unknown>(callback: T, deps: readonly unknown[]): T;
  export function useEffect(effect: () => void | (() => void), deps?: readonly unknown[]): void;
  export function useId(): string;
  export function useMemo<T>(factory: () => T, deps: readonly unknown[]): T;
  export function useRef<T>(initial: T | null): RefObject<T | null>;
  export function useRef<T>(initial: T): RefObject<T>;
  export function useState<T = undefined>(): [T | undefined, Dispatch<SetStateAction<T | undefined>>];
  export function useState<T>(initial: T | (() => T)): [T, Dispatch<SetStateAction<T>>];
  export function useSyncExternalStore(subscribe: (listener: () => void) => () => void, getSnapshot: () => string, getServerSnapshot?: () => string): string;
}
declare module "react/jsx-runtime" {
  export function jsx(type: unknown, props: unknown, key?: unknown): unknown;
  export function jsxs(type: unknown, props: unknown, key?: unknown): unknown;
  export const Fragment: unknown;
  export namespace JSX {
    interface IntrinsicAttributes { key?: unknown; }
    interface UIEvent {
      preventDefault(): void;
      target: { value: string; checked: boolean; files?: FileList | null };
    }
    interface IntrinsicProps {
      [property: string]: unknown;
      onChange?: (event: UIEvent) => unknown;
      onSubmit?: (event: UIEvent) => unknown;
      onClick?: (event: UIEvent) => unknown;
    }
    interface IntrinsicElements { [name: string]: IntrinsicProps }
  }
}
declare module "react-dom/client" {
  export function createRoot(container: Element | DocumentFragment): {render(node: unknown): void};
}
declare module "@tanstack/react-query" {
  export class QueryClient { constructor(config?: unknown); invalidateQueries(input?: unknown): Promise<void>; setQueryData(queryKey: readonly unknown[], data: unknown): void; }
  export function QueryClientProvider(props: {client: QueryClient; children?: unknown}): unknown;
  export function useQuery<T>(options: {queryKey: readonly unknown[]; queryFn: () => Promise<T>; staleTime?: number; gcTime?: number; enabled?: boolean; retry?: number | boolean; refetchOnWindowFocus?: boolean; refetchInterval?: number}): {isPending: boolean; isError: boolean; isFetching: boolean; data: T; refetch(): Promise<unknown>};
  export function useQueryClient(): QueryClient;
  export function useMutation<TData = unknown, TVariables = void>(options: {mutationFn: (value: TVariables) => Promise<TData>; onSuccess?: (data: TData, variables: TVariables) => unknown; onError?: (error: unknown, variables: TVariables) => unknown; onSettled?: (data: TData | undefined, error: unknown | null, variables: TVariables) => unknown}): {isPending: boolean; isError: boolean; isSuccess: boolean; error: unknown; mutate(value?: TVariables): void; reset(): void};
}
declare module "vite" { export function defineConfig(config: unknown): unknown; }
declare module "@vitejs/plugin-react" { const react: () => unknown; export default react; }

declare const process: { env: Record<string, string | undefined> };
