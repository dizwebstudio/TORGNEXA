import {renderToStaticMarkup} from "react-dom/server";
import {PublicDocumentationPage, documentationPages, type DocumentationSectionId} from "../pages/PublicDocumentationPage";

export {documentationPages};

export function renderDocumentation(sectionId?: DocumentationSectionId): string {
  return renderToStaticMarkup(<PublicDocumentationPage sectionId={sectionId}/>);
}
