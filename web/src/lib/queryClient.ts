import { QueryClient } from "@tanstack/react-query";

/** Retries are capped with exponential backoff so a failing server is not hammered. */
export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: 2,
        retryDelay: (attempt) => Math.min(1000 * 2 ** attempt, 10_000),
        refetchOnWindowFocus: false,
      },
    },
  });
}
