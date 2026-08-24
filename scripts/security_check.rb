#!/usr/bin/env ruby

require "find"

root = File.expand_path("..", __dir__)
ignored = [".git", ".bundle", "vendor"]
patterns = {
  "GitLab token" => /glpat-[A-Za-z0-9_-]{20,}/,
  "Mattermost webhook" => %r{/hooks/[A-Za-z0-9_-]{12,}}
}
violations = []

Find.find(root) do |path|
  if File.directory?(path)
    Find.prune if ignored.include?(File.basename(path))
    next
  end
  next if File.basename(path) == File.basename(__FILE__)
  next unless File.file?(path)
  content = File.binread(path)
  next unless content.valid_encoding?
  patterns.each do |name, pattern|
    violations << "#{name}: #{path.delete_prefix(root + "/")}" if content.match?(pattern)
  end
end

if violations.empty?
  puts "Security check passed"
else
  warn violations.uniq.join("\n")
  exit 1
end
