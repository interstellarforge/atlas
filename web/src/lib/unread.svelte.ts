import { getInbox } from "./api";

// Unread count shown in the navigation; the inbox page loads the list itself.
export const inbox = $state({ unread: 0 });

export async function refreshInbox(): Promise<void> {
  try {
    inbox.unread = (await getInbox(true)).unread;
  } catch {
    /* the next signal or page load retries */
  }
}
