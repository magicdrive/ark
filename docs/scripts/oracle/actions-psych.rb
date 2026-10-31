# frozen_string_literal: true
#
# The independent oracle of Ark's GitHub Actions reading: Ruby's Psych
# (libyaml) reads each workflow of a repository and prints its structure in
# the form TestRealRepositories (internal/languages/yaml/realrepo_test.go)
# writes from Ark's extraction:
#
#   trigger FILE EVENT
#   job FILE JOB
#   needs FILE JOB NEED        (sorted within the job, as a set)
#   step FILE JOB INDEX uses=USES   (USES "-" for none)
#
# Usage: ruby actions-psych.rb REPOSITORY_ROOT > repo.psych.txt
# It only reads: Psych.safe_load builds no object a tag names, aliases are
# expanded as GitHub expands them. Psych reads YAML 1.1, where the key `on`
# is the boolean true; it is read as "on".
require "psych"

root = ARGV.fetch(0)
files = Dir.glob(File.join(root, ".github", "workflows", "*.{yml,yaml}")).sort
names = lambda do |v|
  case v
  when nil then []
  when Array then v.flat_map { |x| x.is_a?(Hash) || x.is_a?(Array) ? [] : [x.to_s] }
  when Hash then v.keys.map(&:to_s)
  else [v.to_s]
  end
end
files.each do |path|
  file = path.delete_prefix(root.chomp("/") + "/")
  doc = Psych.safe_load(File.read(path), aliases: true, permitted_classes: [], filename: path)
  next unless doc.is_a?(Hash)
  on = doc.key?("on") ? doc["on"] : doc[true]
  names.call(on).each { |e| puts "trigger #{file} #{e}" unless e.empty? }
  jobs = doc["jobs"]
  next unless jobs.is_a?(Hash)
  jobs.each do |id, job|
    id = id.to_s
    next if id.empty?
    puts "job #{file} #{id}"
    next unless job.is_a?(Hash)
    needs = names.call(job["needs"]).reject(&:empty?).sort.uniq
    needs.each { |n| puts "needs #{file} #{id} #{n}" }
    steps = job["steps"]
    next unless steps.is_a?(Array)
    steps.select { |s| s.is_a?(Hash) }.each_with_index do |s, i|
      uses = s["uses"].is_a?(String) && !s["uses"].empty? ? s["uses"] : "-"
      puts "step #{file} #{id} #{i} uses=#{uses}"
    end
  end
end
