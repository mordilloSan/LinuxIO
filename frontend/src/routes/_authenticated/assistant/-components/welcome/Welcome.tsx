import { Icon } from "@iconify/react";

import type { Alert } from "@/api";
import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";

import { welcomeCards } from "./welcomeCards";

import "./welcome.css";

/**
 * A new chat's first screen: what on this server is worth a look, as prompts
 * one click away.
 */
export default function Welcome({
  alerts,
  onAsk,
}: {
  alerts: Alert[];
  onAsk: (prompt: string) => void;
}) {
  const { cards, alertCount } = welcomeCards(alerts);
  return (
    <div className="assistant-welcome">
      <div className="assistant-welcome__intro">
        {alertCount > 0 ? (
          <AppTypography
            className="assistant-welcome__eyebrow"
            color="text.secondary"
          >
            <span aria-hidden className="assistant-welcome__dot" />
            {alertCount} {alertCount === 1 ? "thing" : "things"} worth a look on
            this server
          </AppTypography>
        ) : null}
        <AppTypography
          className="assistant-welcome__title"
          component="h1"
          fontWeight={600}
        >
          What should we look at?
        </AppTypography>
      </div>
      <div className="assistant-welcome__cards">
        {cards.map((card) => (
          <AppButton
            className="assistant-welcome__card"
            color="inherit"
            key={card.key}
            onClick={() => onAsk(card.prompt)}
          >
            <span aria-hidden className="assistant-welcome__icon">
              <Icon icon={card.icon} width={18} />
            </span>
            <span className="assistant-welcome__text">
              <span className="assistant-welcome__card-title">
                {card.title}
              </span>
              <span className="assistant-welcome__hint">{card.hint}</span>
            </span>
          </AppButton>
        ))}
      </div>
    </div>
  );
}
