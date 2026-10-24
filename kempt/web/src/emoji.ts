// A compact emoji set with names for search and :shortcode: completion.

const raw: Record<string, string> = {
  People:
    "😀 grinning|😃 smiley|😄 smile|😁 grin|😆 laughing|😅 sweat_smile|🤣 rofl|😂 joy|🙂 slight_smile|🙃 upside_down|😉 wink|😊 blush|😇 innocent|🥰 smiling_hearts|😍 heart_eyes|🤩 star_struck|😘 kissing_heart|😋 yum|😛 tongue|😜 winking_tongue|🤪 zany|😝 squinting_tongue|🤑 money_mouth|🤗 hugging|🤭 hand_over_mouth|🤫 shush|🤔 thinking|🤐 zipper_mouth|🤨 raised_eyebrow|😐 neutral|😑 expressionless|😶 no_mouth|😏 smirk|😒 unamused|🙄 eye_roll|😬 grimace|😮‍💨 exhale|🤥 lying|😌 relieved|😔 pensive|😪 sleepy|🤤 drool|😴 sleeping|😷 mask|🤒 sick|🤕 hurt|🤢 nauseated|🤮 vomit|🥵 hot|🥶 cold|🥴 woozy|😵 dizzy|🤯 mind_blown|🤠 cowboy|🥳 party|😎 sunglasses|🤓 nerd|🧐 monocle|😕 confused|😟 worried|🙁 slight_frown|😮 open_mouth|😯 hushed|😲 astonished|😳 flushed|🥺 pleading|😦 frowning|😧 anguished|😨 fearful|😰 anxious|😥 sad_relieved|😢 cry|😭 sob|😱 scream|😖 confounded|😣 persevere|😞 disappointed|😓 sweat|😩 weary|😫 tired|🥱 yawn|😤 triumph|😡 rage|😠 angry|🤬 cursing|😈 smiling_imp|💀 skull|💩 poop|🤡 clown|👻 ghost|👽 alien|🤖 robot|😺 cat_smile|🙈 see_no_evil|🙉 hear_no_evil|🙊 speak_no_evil",
  Gestures:
    "👋 wave|🤚 raised_back|✋ raised_hand|🖖 vulcan|👌 ok_hand|🤌 pinched|🤏 pinching|✌️ v|🤞 crossed_fingers|🤟 love_you|🤘 metal|🤙 call_me|👈 point_left|👉 point_right|👆 point_up|👇 point_down|☝️ index_up|👍 thumbsup|👎 thumbsdown|✊ fist|👊 punch|🤛 left_fist|🤜 right_fist|👏 clap|🙌 raised_hands|👐 open_hands|🤲 palms_up|🤝 handshake|🙏 pray|✍️ writing|💪 muscle|🫡 salute|🫶 heart_hands|👀 eyes|🧠 brain|🫠 melting",
  Hearts: "❤️ heart|🧡 orange_heart|💛 yellow_heart|💚 green_heart|💙 blue_heart|💜 purple_heart|🖤 black_heart|🤍 white_heart|🤎 brown_heart|💔 broken_heart|❣️ heart_exclamation|💕 two_hearts|💞 revolving_hearts|💓 heartbeat|💗 heartpulse|💖 sparkling_heart|💘 cupid|💝 gift_heart|💯 100|💢 anger|💥 boom|💫 dizzy_star|💦 sweat_drops|💨 dash|💬 speech|💤 zzz",
  Things:
    "🔥 fire|✨ sparkles|⭐ star|🌟 star2|⚡ zap|🎉 tada|🎊 confetti|🎁 gift|🏆 trophy|🥇 first_place|🎮 video_game|🕹️ joystick|🎲 dice|🎯 dart|🎵 music|🎶 notes|🎧 headphones|🎤 microphone|📞 phone|💻 computer|🖥️ desktop|⌨️ keyboard|🖱️ mouse|📱 mobile|📷 camera|🎥 movie_camera|📺 tv|💡 bulb|🔔 bell|🔕 no_bell|📌 pushpin|📎 paperclip|🔒 lock|🔑 key|🛠️ tools|⚙️ gear|🧰 toolbox|💰 moneybag|💸 money_wings|📈 chart_up|📉 chart_down|📅 calendar|⏰ alarm|⌛ hourglass|🚀 rocket|🚗 car|✈️ airplane|🏠 house|🍕 pizza|🍔 burger|🌮 taco|🍟 fries|🍿 popcorn|🍩 donut|🍪 cookie|🎂 cake|☕ coffee|🍺 beer|🍻 beers|🍷 wine|🥤 cup",
  Nature: "🐶 dog|🐱 cat|🐭 mouse_face|🦊 fox|🐻 bear|🐼 panda|🐸 frog|🐵 monkey|🐔 chicken|🐧 penguin|🐢 turtle|🐍 snake|🦖 trex|🐙 octopus|🦋 butterfly|🐝 bee|🌸 blossom|🌹 rose|🌻 sunflower|🌲 tree|🍀 clover|🌈 rainbow|☀️ sun|🌙 moon|☁️ cloud|🌧️ rain|❄️ snowflake|🌊 wave_water",
  Symbols: "✅ white_check_mark|☑️ ballot_check|✔️ check|❌ x|❎ x_box|⚠️ warning|⛔ no_entry|🚫 prohibited|❓ question|❗ exclamation|‼️ bangbang|➕ plus|➖ minus|🆗 ok|🆒 cool|🆕 new|🆘 sos|🔴 red_circle|🟠 orange_circle|🟡 yellow_circle|🟢 green_circle|🔵 blue_circle|🟣 purple_circle|⚫ black_circle|⚪ white_circle|⬆️ up|⬇️ down|➡️ right|⬅️ left|🔁 repeat|♻️ recycle|🏁 checkered_flag|🚩 flag",
};

export const emojiCategories: { name: string; list: { e: string; n: string }[] }[] = Object.entries(raw).map(([name, s]) => ({
  name,
  list: s.split("|").map((p) => {
    const [e, n] = p.split(" ");
    return { e, n };
  }),
}));

export const allEmoji = emojiCategories.flatMap((c) => c.list);

export function searchEmoji(q: string, limit = 8) {
  q = q.toLowerCase();
  const starts = allEmoji.filter((x) => x.n.startsWith(q));
  const has = allEmoji.filter((x) => !x.n.startsWith(q) && x.n.includes(q));
  return [...starts, ...has].slice(0, limit);
}

const recentKey = "kempt.recentEmoji";
export function recentEmoji(): string[] {
  try {
    return JSON.parse(localStorage.getItem(recentKey) || "[]");
  } catch {
    return [];
  }
}
export function rememberEmoji(e: string) {
  const r = [e, ...recentEmoji().filter((x) => x !== e)].slice(0, 16);
  localStorage.setItem(recentKey, JSON.stringify(r));
}

export const quickReactions = ["👍", "❤️", "😂", "😮", "😢", "🔥"];
