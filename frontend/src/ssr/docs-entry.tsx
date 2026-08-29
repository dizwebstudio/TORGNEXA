import {renderToStaticMarkup} from "react-dom/server";
import {PublicDocumentationPage, documentationPages, troubleshootingFaq, type DocumentationSectionId} from "../pages/PublicDocumentationPage";

export {documentationPages, troubleshootingFaq};

export function renderDocumentation(sectionId?: DocumentationSectionId): string {
  return renderToStaticMarkup(<PublicDocumentationPage sectionId={sectionId}/>);
}
