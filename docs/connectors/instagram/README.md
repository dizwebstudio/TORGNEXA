# Instagram Connector

Task 043 adds Instagram professional-account publishing on top of Task-020 Social Core.

The admitted baseline publishes one JPEG image, JPEG image carousels, and one MP4 Reel through the official Instagram API with Instagram Login. Text-only posts, Stories, mixed image/video carousels, comments, insights and destructive edit/delete are outside this admission.

Task-020 remains owner of Content/Variant/Publication/scheduling/audit state. Task-088 remains owner of media release decisions. Because Meta fetches `image_url`/`video_url`, the provider uses a host-supplied short-lived HTTPS `MediaStager`; internal object keys and credentials never become provider parameters.
