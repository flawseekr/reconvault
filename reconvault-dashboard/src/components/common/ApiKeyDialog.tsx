import { zodResolver } from "@hookform/resolvers/zod";
import { KeyRoundIcon } from "lucide-react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { setApiKey } from "@/lib/api-key";

const schema = z.object({
  apiKey: z.string().trim().min(32, "API keys are at least 32 characters long."),
});
type FormValues = z.infer<typeof schema>;

interface ApiKeyDialogProps {
  open: boolean;
  /** Absent when a key is required: the dialog then cannot be dismissed. */
  onOpenChange?: (open: boolean) => void;
}

export function ApiKeyDialog({ open, onOpenChange }: ApiKeyDialogProps) {
  const dismissible = onOpenChange !== undefined;
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: { apiKey: "" } });

  function onSubmit(values: FormValues) {
    setApiKey(values.apiKey);
    reset();
    onOpenChange?.(false);
  }

  return (
    <Dialog open={open} onOpenChange={dismissible ? onOpenChange : undefined}>
      <DialogContent
        showClose={dismissible}
        onEscapeKeyDown={(e) => !dismissible && e.preventDefault()}
        onPointerDownOutside={(e) => !dismissible && e.preventDefault()}
      >
        <form onSubmit={handleSubmit(onSubmit)} className="grid gap-4" noValidate>
          <DialogHeader>
            <div className="mb-1 flex size-9 items-center justify-center rounded-lg border border-primary/30 bg-primary/10">
              <KeyRoundIcon className="size-4 text-primary" aria-hidden="true" />
            </div>
            <DialogTitle>Connect to ReconVault</DialogTitle>
            <DialogDescription>
              Enter the API key configured on the server (<code className="font-mono">API_KEY</code>). It is kept in
              this browser tab only and cleared when the tab closes.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-2">
            <Label htmlFor="api-key">API key</Label>
            <Input
              id="api-key"
              type="password"
              autoComplete="off"
              spellCheck={false}
              autoFocus
              className="font-mono"
              aria-invalid={errors.apiKey ? true : undefined}
              aria-describedby={errors.apiKey ? "api-key-error" : undefined}
              {...register("apiKey")}
            />
            {errors.apiKey && (
              <p id="api-key-error" className="text-xs text-destructive">
                {errors.apiKey.message}
              </p>
            )}
          </div>
          <DialogFooter>
            <Button type="submit">Connect</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
