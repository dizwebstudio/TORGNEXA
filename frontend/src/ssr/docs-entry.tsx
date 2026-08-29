import {renderToStaticMarkup} from "react-dom/server";
import {PublicDocumentationPage} from "../pages/PublicDocumentationPage";

export function renderDocumentation(): string {
  return renderToStaticMarkup(<PublicDocumentationPage/>);
}
