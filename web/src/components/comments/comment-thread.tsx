"use client";

import { useState, useEffect, useCallback } from "react";
import type { Comment } from "@/lib/api/types";
import { client } from "@/lib/api/client";
import { useTaskStore } from "@/stores/task-store";
import { CommentComposer } from "./comment-composer";
import { CommentItem } from "./comment-item";

interface CommentThreadProps {
  taskId: number;
  isRunning?: boolean;
  /**
   * Changing this refetches the thread. Comments can be created outside this
   * component — responding to a human request files one — and without a nudge
   * the human's own answer visibly vanishes.
   */
  refreshKey?: number | string;
}

export function CommentThread({ taskId, isRunning, refreshKey }: CommentThreadProps) {
  const fetchTasks = useTaskStore((s) => s.fetchTasks);
  const [comments, setComments] = useState<Comment[]>([]);

  const fetchComments = useCallback(async () => {
    try {
      const data = await client.listComments(taskId);
      setComments([...data].reverse());
    } catch {
      // ignore
    }
  }, [taskId]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async fetch: setState runs after an await, not synchronously
    fetchComments();
  }, [fetchComments, refreshKey]);

  // Optimistic prepend (the thread is newest-first), then reconcile with the
  // server and refresh the task's comment count.
  const handleCreated = (created: Comment) => {
    setComments((prev) => [created, ...prev]);
    fetchComments();
    fetchTasks();
  };

  return (
    <div className="flex flex-col gap-2">
      <CommentComposer taskId={taskId} isRunning={isRunning} onCreated={handleCreated} />

      {comments.length === 0 ? (
        <p className="text-xs text-muted text-center py-4">No comments yet</p>
      ) : (
        comments.map((c) => <CommentItem key={c.id} comment={c} />)
      )}
    </div>
  );
}
