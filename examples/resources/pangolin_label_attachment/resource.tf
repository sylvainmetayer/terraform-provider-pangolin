resource "pangolin_label_attachment" "immich" {
  label_id    = pangolin_label.media.id
  resource_id = pangolin_resource.immich.id
}
