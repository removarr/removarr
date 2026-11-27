import { QueryClient } from "@tanstack/react-query";

// makeQueryClient builds the client the app uses; tests build their own with it. A request
// that changes something is never repeated on its own: the server may refuse it because it
// is busy, and the user decides whether to try again.
export function makeQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: 1 },
      mutations: { retry: false },
    },
  });
}
