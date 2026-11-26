import { useActions } from "@/api/queries";
import { ActionCard } from "@/components/history/ActionCard";
import { Button } from "@/components/ui/button";

export function HistoryPage() {
  const q = useActions();
  const actions = q.data?.pages.flatMap((p) => p.actions) ?? [];
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-2xl font-semibold">History</h1>
      {q.isError && <p role="alert" className="text-sm text-destructive">{q.error.message}</p>}
      {q.isSuccess && actions.length === 0 && <p className="text-sm text-muted-foreground">Nothing was deleted yet.</p>}
      <ul className="flex flex-col gap-4">
        {actions.map((a) => (
          <ActionCard key={a.id} action={a} />
        ))}
      </ul>
      {q.hasNextPage && (
        <Button variant="outline" className="self-start" disabled={q.isFetchingNextPage} onClick={() => q.fetchNextPage()}>
          Load more
        </Button>
      )}
    </div>
  );
}
