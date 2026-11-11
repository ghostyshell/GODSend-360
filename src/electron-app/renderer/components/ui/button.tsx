import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../../lib/utils";

const buttonVariants = cva(
  [
    "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-[10px] text-[13px] font-medium",
    "cursor-pointer select-none",
    "transition-[background-color,border-color,color,box-shadow,transform] duration-200 ease-[cubic-bezier(.22,.8,.24,1)]",
    "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background",
    "active:translate-y-px disabled:pointer-events-none disabled:opacity-45",
  ].join(" "),
  {
    variants: {
      variant: {
        default:
          "bg-white/[0.03] text-foreground border border-white/[0.09] hover:bg-white/[0.07] hover:border-white/[0.18]",
        primary:
          "btn-laser border border-white/20 hover:brightness-110",
        ghost:
          "text-muted-foreground hover:bg-white/[0.06] hover:text-foreground",
        outline:
          "border border-white/[0.16] bg-transparent text-foreground hover:bg-white/[0.06]",
        secondary:
          "bg-white/[0.06] text-foreground border border-white/[0.09] hover:bg-white/[0.09]",
      },
      size: {
        default: "h-8 px-3 py-1.5",
        sm: "h-7 px-2.5 py-1 text-[12px]",
        icon: "h-8 w-8 p-0 text-[17px] rounded-[10px]",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : "button";
    return (
      <Comp
        className={cn(buttonVariants({ variant, size, className }))}
        ref={ref}
        {...props}
      />
    );
  }
);
Button.displayName = "Button";

export { Button, buttonVariants };
