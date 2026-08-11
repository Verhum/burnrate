import { useToastStore } from "@/stores/toast-store";

/**
 * The app's user-feedback channel, callable from anywhere — including stores,
 * which have to be able to report a failure and are not components.
 *
 *   toast.error("Couldn't run BR72", apiErrorMessage(err));
 *   toast.success("BR72 launched");
 *
 * Returns the toast id so a caller can dismiss it early via
 * `useToastStore.getState().dismiss(id)`; most call sites ignore it.
 */
export const toast = {
  success: (title: string, message?: string) =>
    useToastStore.getState().push("success", title, message),
  error: (title: string, message?: string) =>
    useToastStore.getState().push("error", title, message),
  info: (title: string, message?: string) =>
    useToastStore.getState().push("info", title, message),
};
